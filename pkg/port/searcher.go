package port

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

// Searcher is implemented by indices that support full-text search.
type Searcher interface {
	SearchDocuments(ctx context.Context, ddfn *model.DesignDocFn, sq *SearchQuery) (*SearchResult, error)
}
