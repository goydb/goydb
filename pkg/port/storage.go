package port

import (
	"context"
	"time"

	"github.com/goydb/goydb/pkg/model"
)

type AllDocsQuery struct {
	Skip      int64
	Limit     int64
	StartKey  string
	EndKey    string
	SkipLocal bool
	// view options
	DDFN        *model.DesignDocFn
	IncludeDocs bool
	ViewGroup   string
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
