package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// rawTx opens a write transaction with a concrete *Transaction for internal use.
// Use Transaction (port.DatabaseTx callback) for code that should be testable via port.Database.
func (d *Database) rawTx(fn func(tx *Transaction) error) error {
	return d.db.WriteTransaction(func(tx port.EngineWriteTransaction) error {
		return fn(&Transaction{
			EngineWriteTransaction: tx,
			Database:               d,
		})
	})
}

// Transaction implements port.Database.Transaction.
func (d *Database) Transaction(ctx context.Context, fn func(tx port.DatabaseTx) error) error {
	return d.rawTx(func(tx *Transaction) error {
		return fn(tx)
	})
}

func (d *Database) PutDocument(ctx context.Context, doc *model.Document) (string, error) {
	var rev string
	err := d.Transaction(ctx, func(tx port.DatabaseTx) error {
		var err error
		rev, err = tx.PutDocument(ctx, doc)
		return err
	})
	return rev, err
}

func (d *Database) PutDocumentForReplication(ctx context.Context, doc *model.Document) error {
	return d.Transaction(ctx, func(tx port.DatabaseTx) error {
		return tx.PutDocumentForReplication(ctx, doc)
	})
}

func (d *Database) GetDocument(ctx context.Context, docID string) (*model.Document, error) {
	var doc *model.Document
	err := d.Transaction(ctx, func(tx port.DatabaseTx) error {
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
	err := d.Transaction(ctx, func(tx port.DatabaseTx) error {
		var err error
		doc, err = tx.DeleteDocument(ctx, docID, rev)
		return err
	})
	if err != nil {
		return nil, err
	}

	return doc, nil
}
