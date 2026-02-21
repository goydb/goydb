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
	// view options
	DDFN            *model.DesignDocFn
	IncludeDocs     bool
	ViewGroup       string
	// ViewStartKey and ViewEndKey are CBOR-encoded key bounds for view queries.
	// The endkey is already padded for inclusive comparison when set.
	ViewStartKey    []byte
	ViewEndKey      []byte
	ViewExclusiveEnd bool
	// ViewDecodedStartKey and ViewDecodedEndKey hold the decoded (Go interface{})
	// versions of the same bounds for semantic post-filtering using ViewKeyCmp.
	ViewDecodedStartKey interface{}
	ViewDecodedEndKey   interface{}
}

type Observer interface {
	Close()
	WaitForDoc(timeout time.Duration) *model.Document
}

type SearchQuery struct {
	Query    string
	Limit    int
	Skip     int
	Bookmark string
}

type SearchResult struct {
	Total   uint64
	Records []*SearchRecord
}

type SearchRecord struct {
	ID     string
	Order  []float64
	Fields map[string]interface{}
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
