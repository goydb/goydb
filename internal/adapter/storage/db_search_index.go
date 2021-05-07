package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/mapping"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const SearchDir = "search_indices"
const indexExt = ".bleve"

var _ port.SearchIndex = (port.SearchIndex)(nil)

func (d *Database) SearchDocuments(ctx context.Context, ddfn *model.DesignDocFn, sq *port.SearchQuery) (*port.SearchResult, error) {
	si := d.SearchIndex(ddfn)
	if si == nil {
		return nil, ErrNotFound
	}

	sidx := si.(*SearchIndex)

	q := bleve.NewQueryStringQuery(sq.Query)
	searchRequest := bleve.NewSearchRequest(q)
	res, err := sidx.idx.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, err
	}

	var sr port.SearchResult
	sr.Total = res.Total

	for i, hit := range res.Hits {
		sr.Records = append(sr.Records, &port.SearchRecord{
			ID:     hit.ID,
			Order:  []float64{hit.Score, float64(hit.HitNumber)},
			Fields: hit.Fields,
		})

		// respect limit
		if i == sq.Limit-1 {
			break
		}
	}

	return &sr, nil
}

func (d *Database) UpdateSearch(ctx context.Context, ddfn *model.DesignDocFn, docs []*model.SearchIndexDoc) error {
	si, err := d.EnsureSearchIndex(ddfn)
	if err != nil {
		return err
	}

	err = si.UpdateMapping(docs)
	if err != nil {
		return err
	}

	// update search index
	err = si.Tx(func(tx port.SearchIndexTx) error {
		for _, doc := range docs {
			log.Printf("INDEX %s %v %v", doc.ID, doc.Fields, doc.Options)
			err := tx.Index(doc.ID, doc.Fields)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

func (d *Database) SearchIndex(ddfn *model.DesignDocFn) port.SearchIndex {
	d.muSearchIndicies.RLock()
	si, ok := d.searchIndicies[ddfn.String()]
	d.muSearchIndicies.RUnlock()
	if !ok {
		return nil
	}
	return si
}

func (d *Database) EnsureSearchIndex(ddfn *model.DesignDocFn) (port.SearchIndex, error) {
	// check if the index already exists
	si := d.SearchIndex(ddfn)
	if si != nil {
		return si, nil
	}

	// index doesn't exist, create a new index
	d.muSearchIndicies.Lock()
	defer d.muSearchIndicies.Unlock()

	// try to open search from fs
	si, err := d.openSearchIndex(ddfn.String())
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		d.searchIndicies[si.Name()] = si
		return si, nil
	}

	// create new search index for the doc
	si, err = d.createSearchIndex(ddfn.String())
	if err != nil {
		return nil, err
	}

	d.searchIndicies[si.Name()] = si
	return si, nil
}

// openAllSearchIndices load all created database indices
func (d *Database) openAllSearchIndices() error {
	path := filepath.Join(d.databaseDir, SearchDir)

	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read search indices dir %q: %w", path, err)
	}

	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("failed to find search indices in %q: %w", path, err)
	}

	for _, entry := range dirEntries {
		if filepath.Ext(entry.Name()) != indexExt {
			continue
		}

		docID := strings.TrimSuffix(entry.Name(), indexExt)
		si, err := d.openSearchIndex(docID)
		if err != nil {
			log.Printf("skipping, unable to open saearch index, possible corruption: %v", err)
		}

		d.muSearchIndicies.Lock()
		d.searchIndicies[si.Name()] = si
		d.muSearchIndicies.Unlock()
	}

	return nil
}

func (d *Database) searchIndexPath(name string) string {
	return filepath.Join(d.databaseDir, SearchDir, name+indexExt)
}

func (d *Database) openSearchIndex(name string) (port.SearchIndex, error) {
	path := d.searchIndexPath(name)
	_, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	index, err := bleve.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open search index %q: %w", path, index)
	}

	// there is no other way to access the index mapping implementation
	// but it is needed to extend the mapping based on the view output
	mapping, ok := index.Mapping().(*mapping.IndexMappingImpl)
	if !ok {
		return nil, fmt.Errorf("failed to open search index %q unable to load mapping "+
			"implementation invalid type: %v", path, reflect.TypeOf(index.Mapping()))
	}

	return &SearchIndex{
		name:    name,
		idx:     index,
		mapping: mapping,
		path:    path,
	}, nil
}

func (d *Database) createSearchIndex(name string) (port.SearchIndex, error) {
	path := d.searchIndexPath(name)
	mapping := bleve.NewIndexMapping()
	index, err := bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to create search index %q: %w", path, index)
	}
	return &SearchIndex{
		name:    name,
		idx:     index,
		mapping: mapping,
		path:    path,
	}, nil
}

type SearchIndex struct {
	mapping *mapping.IndexMappingImpl
	name    string
	idx     bleve.Index
	path    string
}

type SearchIndexTx struct {
	si *SearchIndex
	tx *bleve.Batch
}

func (si SearchIndex) Name() string {
	return si.name
}

func (si SearchIndex) String() string {
	cnt, err := si.idx.DocCount()
	if err != nil {
		log.Printf("failed to get search index %s count: %v", si.path, err)
	}

	return fmt.Sprintf("<SearchIndex name=%q count=%v>", si.name, cnt)
}

func (si *SearchIndex) Close() error {
	return si.idx.Close()
}

// UpdateMapping can extend the mapping configuration for the index.
// NOT: CouchDB inherited behavior is, that the index can only be extended but
// fields can't be removed, for that the index has to be rebuild.
// Also the configuration of a field can't be changed once given.
func (si *SearchIndex) UpdateMapping(docs []*model.SearchIndexDoc) error {
	// PROCESS merge all provided options into one superset
	cfg := make(map[string]struct{})

	// Step 1 load config from mapping
	for name, _ := range si.mapping.DefaultMapping.Properties {
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
				newType[field] = reflect.TypeOf(doc.Fields[field]).Kind()
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
			log.Printf("fallback to default mapping for %q for index %q", field, si.name)
			continue
		}

		// TODO: set fm.Analyzer
		fm.Store = opt.Store
		fm.Index = opt.ShouldIndex()
		fm.DocValues = opt.Facet // TODO: unsure, verify mapping
		fm.Name = field

		log.Printf("add field mapping for %v, %#v", field, fm)
		si.mapping.DefaultMapping.AddFieldMappingsAt(field, fm)
	}

	return nil
}

func (si *SearchIndex) Tx(fn func(tx port.SearchIndexTx) error) error {
	b := si.idx.NewBatch()
	tx := SearchIndexTx{
		si: si,
		tx: b,
	}

	// fill batch
	err := fn(&tx)
	if err != nil {
		return err
	}

	// execute batch
	return si.idx.Batch(b)
}

func (si *SearchIndexTx) Index(id string, data map[string]interface{}) error {
	return si.tx.Index(id, data)
}

func (si *SearchIndexTx) Delete(id string) error {
	si.tx.Delete(id)
	return nil
}

func (si *SearchIndex) Destroy() error {
	err := si.Close()
	if err != nil {
		log.Printf("failed to close search index %s before destroy: %v", si.path, err)
	}

	return os.RemoveAll(si.path)
}
