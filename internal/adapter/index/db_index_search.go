package index

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"sync"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/mapping"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const (
	searchBucket = "_searches"
)

var _ port.DocumentIndex = (*ExternalSearchIndex)(nil)
var _ port.DocumentIndexSourceUpdate = (*ExternalSearchIndex)(nil)

type ExternalSearchIndex struct {
	path     string
	ddfn     *model.DesignDocFn
	engines  port.ViewEngines
	server   port.ViewServer
	mapping  *mapping.IndexMappingImpl
	idx      bleve.Index
	mu       sync.RWMutex
	SearchFn string
}

func NewExternalSearchIndex(ddfn *model.DesignDocFn, engines port.ViewEngines, path string) *ExternalSearchIndex {
	return &ExternalSearchIndex{
		path:    path,
		ddfn:    ddfn,
		engines: engines,
	}
}

func (i *ExternalSearchIndex) String() string {
	if i.idx != nil {
		cnt, err := i.idx.DocCount()
		if err != nil {
			log.Printf("failed to get search index %s count: %v", i.path, err)
		}
		return fmt.Sprintf("<ExternalSearchIndex name=%q count=%v>", i.ddfn, cnt)
	}

	return fmt.Sprintf("<ExternalSearchIndex name=%q>", i.ddfn)
}

func (i *ExternalSearchIndex) UpdateSource(ctx context.Context, doc *model.Document, f *model.Function) error {
	searchFn := f.SearchFn
	language := doc.Language()

	if searchFn == "" {
		return errors.New("invalid empty search function")
	}

	// if the mapFn is the same, to nothing
	if i.SearchFn == searchFn {
		return nil
	}

	// func is different, build the view server
	builder, ok := i.engines[language]
	if !ok {
		return fmt.Errorf("search engine for language %q is not registered", language)
	}
	vs, err := builder(searchFn)
	if err != nil {
		return fmt.Errorf("failed to compile search: %w", err)
	}

	// view was successfully created, update mapFn and viewServer
	i.mu.Lock()
	i.SearchFn = searchFn
	i.server = vs
	i.mu.Unlock()

	return nil
}

func (i *ExternalSearchIndex) SourceType() model.FnType {
	return model.SearchFn
}

func (i *ExternalSearchIndex) Ensure(ctx context.Context, tx port.EngineWriteTransaction) error {
	// make sure the index is only initialized once
	i.mu.RLock()
	idx := i.idx
	i.mu.RUnlock()
	if idx != nil {
		return nil
	}

	_, err := os.Stat(i.path)
	if errors.Is(err, os.ErrNotExist) {
		// needs to be created
		mapping := bleve.NewIndexMapping()
		index, err := bleve.New(i.path, mapping)
		if err != nil {
			return fmt.Errorf("failed to create search index %q: %w", i.path, err)
		}
		i.mapping = mapping
		i.idx = index
		return nil
	} else if err != nil {
		return err
	}

	// needs to be opened
	index, err := bleve.Open(i.path)
	if err != nil {
		return fmt.Errorf("failed to open search index %q: %w", i.path, index)
	}

	// there is no other way to access the index mapping implementation
	// but it is needed to extend the mapping based on the view output
	mapping, ok := index.Mapping().(*mapping.IndexMappingImpl)
	if !ok {
		return fmt.Errorf("failed to open search index %q unable to load mapping "+
			"implementation invalid type: %v", i.path, reflect.TypeOf(index.Mapping()))
	}

	i.mapping = mapping
	i.idx = index
	return nil
}

func (i *ExternalSearchIndex) Remove(ctx context.Context, tx port.EngineWriteTransaction) error {
	err := i.idx.Close()
	if err != nil {
		log.Printf("failed to close search index %s before destroy: %v", i.path, err)
	}

	return os.RemoveAll(i.path)
}

func (i *ExternalSearchIndex) Stats(ctx context.Context, tx port.EngineReadTransaction) (*model.IndexStats, error) {
	docCnt, err := i.idx.DocCount()
	if err != nil {
		return nil, err
	}

	stats := model.IndexStats{
		Documents: docCnt,
		Keys:      docCnt,
	}

	f, err := os.Stat(i.path)
	if err == nil {
		stats.Used = uint64(f.Size())
	}

	// FIXME: try to find a way to get alloc bytes

	return &stats, nil
}

func (i *ExternalSearchIndex) DocumentStored(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	// ignore deleted docs, don't re-index them again
	if doc.Deleted {
		return nil
	}

	return i.UpdateStored(ctx, tx, []*model.Document{doc})
}

func (i *ExternalSearchIndex) UpdateStored(ctx context.Context, tx port.EngineWriteTransaction, docs []*model.Document) error {
	err := i.UpdateMapping(docs)
	if err != nil {
		return err
	}

	// get view server
	i.mu.RLock()
	vs := i.server
	i.mu.RUnlock()

	b := i.idx.NewBatch()

	// execute document against view server
	searchDocs, err := vs.ExecuteSearch(ctx, docs)
	if err != nil {
		return err
	}

	// update search index
	for _, doc := range searchDocs {
		// log.Println("Index", doc.ID, doc.Fields)
		err := b.Index(doc.ID, doc.Fields)
		if err != nil {
			return err
		}
	}

	err = i.idx.Batch(b)
	return err
}

func (i *ExternalSearchIndex) DocumentDeleted(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	return i.idx.Delete(doc.ID)
}

func (i *ExternalSearchIndex) IteratorOptions(ctx context.Context) (*model.IteratorOptions, error) {
	panic("not implemented use SearchDocuments instead")
}

func (i *ExternalSearchIndex) SearchDocuments(ctx context.Context, ddfn *model.DesignDocFn, sq *port.SearchQuery) (*port.SearchResult, error) {
	q := bleve.NewQueryStringQuery(sq.Query)
	searchRequest := bleve.NewSearchRequestOptions(q, sq.Limit, sq.Skip, false)
	searchRequest.Fields = []string{"*"}
	res, err := i.idx.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, err
	}

	var sr port.SearchResult
	sr.Total = res.Total

	for _, hit := range res.Hits {
		sr.Records = append(sr.Records, &port.SearchRecord{
			ID:     hit.ID,
			Order:  []float64{hit.Score, float64(hit.HitNumber)},
			Fields: hit.Fields,
		})
	}

	return &sr, nil
}

// UpdateMapping can extend the mapping configuration for the index.
// NOT: CouchDB inherited behavior is, that the index can only be extended but
// fields can't be removed, for that the index has to be rebuild.
// Also the configuration of a field can't be changed once given.
func (i *ExternalSearchIndex) UpdateMapping(docs []*model.Document) error {
	// PROCESS merge all provided options into one superset
	cfg := make(map[string]struct{})

	// Step 1 load config from mapping
	for name, _ := range i.mapping.DefaultMapping.Properties {
		cfg[name] = struct{}{}
	}

	// update the mapping based on docs[*].Options
	// assumption, first config wins
	newCfg := make(map[string]model.SearchIndexOption)
	newType := make(map[string]reflect.Kind)
	for _, doc := range docs {
		for field, opt := range doc.Options {
			// ignore already existing fields from the mapping
			if _, ok := cfg[field]; ok {
				continue
			}

			// store options for new field, unless
			// we already have a config
			if _, ok := newCfg[field]; !ok {
				newCfg[field] = opt
				docField := doc.Fields[field]
				if docField != nil {
					newType[field] = reflect.TypeOf(docField).Kind()
				}
			}
		}
	}

	// update the mapping (add new not yet mapped fields)
	for field, opt := range newCfg {
		// update mapping
		var fm *mapping.FieldMapping
		switch newType[field] {
		case reflect.Bool:
			fm = mapping.NewBooleanFieldMapping()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
			reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64:
			fm = mapping.NewNumericFieldMapping()
		case reflect.String:
			fm = mapping.NewTextFieldMapping()
		default:
			// we can't add a dedicated mapping here
			// using default mapping mechanism as a fallback
			log.Printf("fallback to default mapping for %q for index %q", field, i.ddfn)
			continue
		}

		// TODO: set fm.Analyzer
		fm.Store = opt.Store
		fm.Index = opt.ShouldIndex()
		fm.DocValues = opt.Facet // TODO: unsure, verify mapping
		fm.Name = field

		log.Printf("add field mapping for %v, %#v", field, fm)
		i.mapping.DefaultMapping.AddFieldMappingsAt(field, fm)
	}

	return nil
}
