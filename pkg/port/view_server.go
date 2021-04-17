package port

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

type ViewServer interface {
	Process(ctx context.Context, docs []*model.Document) ([]*model.Document, error)
}
