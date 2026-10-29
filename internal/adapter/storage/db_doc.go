package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// rawTx opens a write transaction with a concrete *Transaction for internal use.
// Use Transaction (port.DatabaseTx callback) for code that should be testable via port.Database.
func (d *Database) rawTx(fn func(tx *Transaction) error) error {
	return d.db.WriteTransaction(d.logger, func(tx port.EngineWriteTransaction) error {
		return fn(&Transaction{
			EngineWriteTransaction: tx,
			Database:               d,
		})
	})
}

// Transaction implements port.Database.Transaction. Every document written
// or deleted through the port.DatabaseTx handed to fn -- however fn chooses
// to use it -- is announced via NotifyDocumentUpdate once the transaction
// commits successfully, so callers that open their own transaction (bulk
// writes, attachment updates, replication, etc.) get the same change-
// listener/WaitForChange notification guarantee as the top-level
// PutDocument/PutDocumentForReplication/DeleteDocument convenience methods.
func (d *Database) Transaction(ctx context.Context, fn func(tx port.DatabaseTx) error) error {
	var notifications []*model.Document
	err := d.rawTx(func(tx *Transaction) error {
		err := fn(tx)
		notifications = tx.notifications
		return err
	})
	if err == nil {
		for _, doc := range notifications {
			d.NotifyDocumentUpdate(doc)
		}
	}
	return err
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

func (d *Database) GetLeaves(ctx context.Context, docID string) ([]*model.Document, error) {
	var leaves []*model.Document
	err := d.Transaction(ctx, func(tx port.DatabaseTx) error {
		var err error
		leaves, err = tx.GetLeaves(ctx, docID)
		return err
	})
	return leaves, err
}

func (d *Database) GetLeaf(ctx context.Context, docID, rev string) (*model.Document, error) {
	var leaf *model.Document
	err := d.Transaction(ctx, func(tx port.DatabaseTx) error {
		var err error
		leaf, err = tx.GetLeaf(ctx, docID, rev)
		return err
	})
	return leaf, err
}
