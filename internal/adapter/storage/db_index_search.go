package storage

import (
	"context"
	"fmt"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.DocumentIndex = (*ExternalSearchIndex)(nil)
var _ port.DocumentIndexSourceUpdate = (*ExternalSearchIndex)(nil)

type ExternalSearchIndex struct {
	ddfn    *model.DesignDocFn
	engines port.ViewEngines
}

func NewExternalSearchIndex(ddfn *model.DesignDocFn, engines port.ViewEngines) *ExternalSearchIndex {
	return &ExternalSearchIndex{
		ddfn:    ddfn,
		engines: engines,
	}
}

func (i *ExternalSearchIndex) String() string {
	return fmt.Sprintf("<ExternalSearchIndex name=%q>", i.ddfn)
}

func (i *ExternalSearchIndex) UpdateSource(ctx context.Context, doc *model.Document, sf *model.Function) error {
	panic("not implemented") // TODO: Implement
}

func (i *ExternalSearchIndex) SourceType() model.FnType {
	return model.SearchFn
}

func (i *ExternalSearchIndex) Ensure(ctx context.Context, tx port.Transaction) error {
	panic("not implemented") // TODO: Implement
}

func (i *ExternalSearchIndex) Remove(ctx context.Context, tx port.Transaction) error {
	panic("not implemented") // TODO: Implement
}

func (i *ExternalSearchIndex) Stats(ctx context.Context, tx port.Transaction) (*model.IndexStats, error) {
	panic("not implemented") // TODO: Implement
}

func (i *ExternalSearchIndex) DocumentStored(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	panic("not implemented") // TODO: Implement
}

func (i *ExternalSearchIndex) UpdateStored(ctx context.Context, tx port.Transaction, docs []*model.Document) error {
	panic("not implemented") // TODO: Implement
}

func (i *ExternalSearchIndex) DocumentDeleted(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	panic("not implemented") // TODO: Implement
}

func (i *ExternalSearchIndex) Iterator(ctx context.Context, tx port.Transaction) (port.Iterator, error) {
	panic("not implemented") // TODO: Implement
}
