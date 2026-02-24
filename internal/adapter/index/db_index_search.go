package index

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search/highlight"
	simpleFragmenter "github.com/blevesearch/bleve/v2/search/highlight/fragmenter/simple"
	"github.com/blevesearch/bleve/v2/search/highlight/format/html"
	"github.com/blevesearch/bleve/v2/search/highlight/highlighter/simple"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
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
	logger   port.Logger
}

func NewExternalSearchIndex(ddfn *model.DesignDocFn, engines port.ViewEngines, path string, logger port.Logger) *ExternalSearchIndex {
	return &ExternalSearchIndex{
		path:    path,
		ddfn:    ddfn,
		engines: engines,
		logger:  logger,
	}
}

func (i *ExternalSearchIndex) String() string {
	if i.idx != nil {
		cnt, err := i.idx.DocCount()
		if err != nil {
			i.logger.Warnf(context.Background(), "search index count failed", "error", err)
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
		return fmt.Errorf("failed to open search index %q: %w", i.path, err)
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
		i.logger.Warnf(ctx, "search index close failed", "error", err)
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
	// get view server
	i.mu.RLock()
	vs := i.server
	i.mu.RUnlock()

	// execute document against view server
	searchDocs, err := vs.ExecuteSearch(ctx, docs)
	if err != nil {
		return err
	}

	// update field mapping from search output (Fields/Options are only
	// populated after ExecuteSearch, not on the raw input docs)
	err = i.UpdateMapping(searchDocs)
	if err != nil {
		return err
	}

	b := i.idx.NewBatch()

	// update search index
	for _, doc := range searchDocs {
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
	q, err := parseLuceneQuery(sq.Query)
	if err != nil {
		return nil, fmt.Errorf("invalid search query: %w", err)
	}

	// Apply drilldown filters as additional AND constraints on the query.
	if len(sq.Drilldown) > 0 {
		q = applyDrilldown(q, sq.Drilldown)
	}

	// Determine limit and offset. For grouping, fetch a large set internally.
	limit := sq.Limit
	offset := sq.Skip
	if sq.GroupField != "" {
		limit = 10000 // internal cap for post-process grouping
		offset = 0
	}

	// Decode bookmark → offset.
	if sq.Bookmark != "" {
		if decoded, err := base64.StdEncoding.DecodeString(sq.Bookmark); err == nil {
			if n, err := strconv.Atoi(string(decoded)); err == nil && n > 0 {
				offset = n
			}
		}
	}

	searchRequest := bleve.NewSearchRequestOptions(q, limit, offset, false)

	// Fields to return.
	if len(sq.IncludeFields) > 0 {
		searchRequest.Fields = sq.IncludeFields
	} else {
		searchRequest.Fields = []string{"*"}
	}

	// Sort.
	if len(sq.Sort) > 0 {
		searchRequest.SortBy(sq.Sort)
	}

	// Facets for counts.
	for _, field := range sq.Counts {
		searchRequest.AddFacet(field, bleve.NewFacetRequest(field, 1000))
	}

	// Facets for ranges.
	for field, ranges := range sq.Ranges {
		facet := bleve.NewFacetRequest(field, len(ranges))
		for _, r := range ranges {
			facet.AddNumericRange(r.Label, r.Min, r.Max)
		}
		searchRequest.AddFacet(field, facet)
	}

	// Highlighting.
	if len(sq.HighlightFields) > 0 {
		preTag := sq.HighlightPreTag
		postTag := sq.HighlightPostTag
		if preTag == "" {
			preTag = "<em>"
		}
		if postTag == "" {
			postTag = "</em>"
		}
		formatter := html.NewFragmentFormatter(preTag, postTag)
		fragmenter := simpleFragmenter.NewFragmenter(200)
		// Register a custom highlighter that uses our fragment formatter.
		highlighterName := "goydb_custom"
		_ = registry.RegisterHighlighter(highlighterName, func(config map[string]interface{}, cache *registry.Cache) (highlight.Highlighter, error) {
			return simple.NewHighlighter(fragmenter, formatter, simple.DefaultSeparator), nil
		})
		hl := bleve.NewHighlightWithStyle(highlighterName)
		for _, f := range sq.HighlightFields {
			hl.AddField(f)
		}
		searchRequest.Highlight = hl
	}

	res, err := i.idx.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, err
	}

	var sr port.SearchResult
	sr.Total = res.Total

	// Encode next-page bookmark.
	nextOffset := offset + len(res.Hits)
	if uint64(nextOffset) < res.Total {
		sr.Bookmark = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(nextOffset)))
	}

	// Map facet results → counts and ranges.
	if len(res.Facets) > 0 {
		sr.Counts = make(map[string]map[string]int)
		sr.Ranges = make(map[string]map[string]int)
		for name, facet := range res.Facets {
			if facet.Terms != nil && facet.Terms.Len() > 0 {
				m := make(map[string]int)
				for _, t := range facet.Terms.Terms() {
					m[t.Term] = t.Count
				}
				sr.Counts[name] = m
			}
			if len(facet.NumericRanges) > 0 {
				m := make(map[string]int)
				for _, r := range facet.NumericRanges {
					m[r.Name] = r.Count
				}
				sr.Ranges[name] = m
			}
		}
		if len(sr.Counts) == 0 {
			sr.Counts = nil
		}
		if len(sr.Ranges) == 0 {
			sr.Ranges = nil
		}
	}

	for _, hit := range res.Hits {
		rec := &port.SearchRecord{
			ID:     hit.ID,
			Fields: hit.Fields,
		}
		// Order: use sort values if custom sort is active, otherwise score + hit number.
		if len(sq.Sort) > 0 && len(hit.Sort) > 0 {
			rec.Order = sortValuesToFloats(hit.Sort)
		} else {
			rec.Order = []float64{hit.Score, float64(hit.HitNumber)}
		}
		// Highlights.
		if len(hit.Fragments) > 0 {
			rec.Highlights = make(map[string][]string)
			for field, frags := range hit.Fragments {
				rec.Highlights[field] = frags
			}
		}
		sr.Records = append(sr.Records, rec)
	}

	// Post-process grouping.
	if sq.GroupField != "" {
		sr = applyGrouping(sr, sq)
	}

	return &sr, nil
}

// applyDrilldown combines the main query with drilldown constraints.
// Each drilldown entry ["field", "val1", "val2", ...] creates a disjunction
// of MatchQuery for that field; all drilldown entries are ANDed together.
func applyDrilldown(mainQuery query.Query, drilldown [][]string) query.Query {
	conjuncts := []query.Query{mainQuery}
	for _, dd := range drilldown {
		if len(dd) < 2 {
			continue
		}
		field := dd[0]
		values := dd[1:]
		var disjuncts []query.Query
		for _, v := range values {
			mq := query.NewMatchQuery(v)
			mq.SetField(field)
			disjuncts = append(disjuncts, mq)
		}
		if len(disjuncts) == 1 {
			conjuncts = append(conjuncts, disjuncts[0])
		} else {
			conjuncts = append(conjuncts, query.NewDisjunctionQuery(disjuncts))
		}
	}
	if len(conjuncts) == 1 {
		return mainQuery
	}
	return query.NewConjunctionQuery(conjuncts)
}

// sortValuesToFloats converts Bleve sort interface values to float64 slice.
func sortValuesToFloats(vals []string) []float64 {
	out := make([]float64, len(vals))
	for i, v := range vals {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			out[i] = f
		}
	}
	return out
}

// applyGrouping post-processes search results into groups by field value.
func applyGrouping(sr port.SearchResult, sq *port.SearchQuery) port.SearchResult {
	groupLimit := sq.GroupLimit
	if groupLimit <= 0 {
		groupLimit = 1
	}

	groups := make(map[string]*port.SearchGroup)
	var order []string

	for _, rec := range sr.Records {
		val := ""
		if rec.Fields != nil {
			if v, ok := rec.Fields[sq.GroupField]; ok {
				val = fmt.Sprintf("%v", v)
			}
		}
		g, exists := groups[val]
		if !exists {
			g = &port.SearchGroup{By: val}
			groups[val] = g
			order = append(order, val)
		}
		g.TotalRows++
		if len(g.Rows) < groupLimit {
			g.Rows = append(g.Rows, rec)
		}
	}

	result := port.SearchResult{
		Total:  sr.Total,
		Groups: make([]port.SearchGroup, 0, len(order)),
	}
	for _, key := range order {
		result.Groups = append(result.Groups, *groups[key])
	}
	return result
}

// UpdateMapping can extend the mapping configuration for the index.
// NOT: CouchDB inherited behavior is, that the index can only be extended but
// fields can't be removed, for that the index has to be rebuild.
// Also the configuration of a field can't be changed once given.
func (i *ExternalSearchIndex) UpdateMapping(docs []*model.Document) error {
	// PROCESS merge all provided options into one superset
	cfg := make(map[string]struct{})

	// Step 1 load config from mapping
	for name := range i.mapping.DefaultMapping.Properties {
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
			i.logger.Debugf(context.Background(), "using default field mapping", "field", field)
			continue
		}

		// TODO: set fm.Analyzer
		fm.Store = opt.Store
		fm.Index = opt.ShouldIndex()
		fm.DocValues = opt.Facet // TODO: unsure, verify mapping
		fm.Name = field

		i.logger.Debugf(context.Background(), "added field mapping", "field", field, "type", fm.Type)
		i.mapping.DefaultMapping.AddFieldMappingsAt(field, fm)
	}

	return nil
}
