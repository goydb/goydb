package port

import (
	"context"
	"time"

	"github.com/goydb/goydb/pkg/model"
)

type AllDocsQuery struct {
	Skip         int64
	Limit        int64
	StartKey     string
	EndKey       string
	SkipLocal    bool
	ExclusiveEnd bool // true when inclusive_end=false
	Descending   bool // reverse iteration order
	// view options
	DDFN            *model.DesignDocFn
	IncludeDocs     bool
	ViewGroup       string
	ViewGroupLevel  int // 0 = not set; 1-N = group by first N array elements
	// ViewStartKey and ViewEndKey are CBOR-encoded key bounds for view queries.
	// The endkey is already padded for inclusive comparison when set.
	ViewStartKey    []byte
	ViewEndKey      []byte
	ViewExclusiveEnd bool
	// ViewDecodedStartKey and ViewDecodedEndKey hold the decoded (Go interface{})
	// versions of the same bounds for semantic post-filtering using ViewKeyCmp.
	ViewDecodedStartKey interface{}
	ViewDecodedEndKey   interface{}
	// ViewDescending reverses iteration order when true.
	ViewDescending bool
	// ViewKeys is an explicit list of keys for multi-key lookup.
	// nil = not set (use range-based iteration).
	ViewKeys []interface{}
	// ViewUpdateSeq includes update_seq in the view response when true.
	ViewUpdateSeq bool
	// ViewOmitSortedInfo when true omits total_rows and offset from the response.
	// Corresponds to the CouchDB sorted=false query parameter.
	ViewOmitSortedInfo bool
}

type Observer interface {
	Close()
	WaitForDoc(timeout time.Duration) *model.Document
}

type SearchQuery struct {
	Query            string
	Limit            int
	Skip             int
	Bookmark         string
	Sort             []string            // ["field1", "-field2"]
	IncludeDocs      bool
	Stale            string              // "ok" or ""
	Counts           []string            // field names for term facets
	Ranges           map[string][]SearchRange // field -> ranges
	Drilldown        [][]string          // [["field","val1","val2"], ...]
	HighlightFields  []string
	HighlightPreTag  string
	HighlightPostTag string
	HighlightNumber  int
	HighlightSize    int
	GroupField       string
	GroupLimit       int
	GroupSort        []string
	IncludeFields    []string
}

// SearchRange defines a numeric range for faceted search.
type SearchRange struct {
	Label string
	Min   *float64
	Max   *float64
}

type SearchResult struct {
	Total    uint64
	Bookmark string
	Records  []*SearchRecord
	Counts   map[string]map[string]int
	Ranges   map[string]map[string]int
	Groups   []SearchGroup
}

// SearchGroup represents a grouped search result.
type SearchGroup struct {
	By        string
	TotalRows int
	Rows      []*SearchRecord
	Bookmark  string
}

type SearchRecord struct {
	ID         string
	Order      []float64
	Fields     map[string]interface{}
	Doc        map[string]interface{}
	Highlights map[string][]string
}

type ChangeListener interface {
	// DocumentChanged a function that handles document change updates
	DocumentChanged(ctx context.Context, doc *model.Document) error
}

// ChangeListenerFunc implements ChangeListener using a simple function
type ChangeListenerFunc func(ctx context.Context, doc *model.Document) error

var _ ChangeListener = (*ChangeListenerFunc)(nil)

func (f ChangeListenerFunc) DocumentChanged(ctx context.Context, doc *model.Document) error {
	return f(ctx, doc)
}
