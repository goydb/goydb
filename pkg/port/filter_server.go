package port

import (
	"context"
	"github.com/goydb/goydb/pkg/model"
)

// FilterEngines maps language names to filter builders
type FilterEngines map[string]FilterServerBuilder

// FilterServerBuilder compiles a filter function into a FilterServer
type FilterServerBuilder func(fn string) (FilterServer, error)

// FilterServer executes a compiled filter function
type FilterServer interface {
	// ExecuteFilter tests if a document passes the filter
	// req contains query parameters, user context, etc.
	ExecuteFilter(ctx context.Context, doc *model.Document, req map[string]interface{}) (bool, error)
}
