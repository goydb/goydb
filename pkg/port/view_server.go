package port

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

type ViewServer interface {
	ExecuteView(ctx context.Context, docs []*model.Document) ([]*model.Document, error)
	ExecuteSearch(ctx context.Context, docs []*model.Document) ([]*model.SearchIndexDoc, error)
}
