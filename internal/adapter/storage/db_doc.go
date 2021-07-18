package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) Transaction(ctx context.Context, fn func(tx *Transaction) error) error {
	return d.db.WriteTransaction(func(tx port.EngineWriteTransaction) error {
		return fn(&Transaction{
			EngineWriteTransaction: tx,
			Database:               d,
		})
	})
}

func (d *Database) PutDocument(ctx context.Context, doc *model.Document) (string, error) {
	var rev string
	err := d.Transaction(ctx, func(tx *Transaction) error {
		var err error
		rev, err = tx.PutDocument(ctx, doc)
		return err
	})
	return rev, err
}

func (d *Database) GetDocument(ctx context.Context, docID string) (*model.Document, error) {
	var doc *model.Document
	err := d.Transaction(ctx, func(tx *Transaction) error {
		var err error
		doc, err = tx.GetDocument(ctx, docID)
		return err
	})
	if err != nil {
		return nil, err
	}

	return doc, nil
}

func (d *Database) DeleteDocument(ctx context.Context, docID, rev string) (*model.Document, error) {
	var doc *model.Document
	err := d.Transaction(ctx, func(tx *Transaction) error {
		var err error
		doc, err = tx.DeleteDocument(ctx, docID, rev)
		return err

	})
	if err != nil {
		return nil, err
	}

	return doc, nil
}
