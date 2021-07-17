package storage

import (
	"context"

	"github.com/goydb/goydb/internal/adapter/bbolt_engine"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) Iterator(ctx context.Context, ddfn *model.DesignDocFn, fn func(i port.Iterator) error) error {
	return d.Transaction(func(tx *Transaction) error {
		tx.
		iter := bbolt_engine.NewIterator(tx, bbolt_engine.ForDesignDocFn(ddfn))
		if iter == nil {
			return nil
		}
		return fn(iter)
	})
}
