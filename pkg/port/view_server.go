package port

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

type ViewEngines map[string]ViewServerBuilder

// ViewServerBuilder build a new ViewServer using the passed map function
type ViewServerBuilder func(fn string) (ViewServer, error)

type ViewServer interface {
	ExecuteView(ctx context.Context, docs []*model.Document) ([]*model.Document, error)
	ExecuteSearch(ctx context.Context, docs []*model.Document) ([]*model.SearchIndexDoc, error)
}
