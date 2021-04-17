package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	bolt "go.etcd.io/bbolt"
)

var docsBucket = []byte("docs")

func (d *Database) Transaction(ctx context.Context, fn func(tx port.Transaction) error) error {
	return d.Update(func(btx *bolt.Tx) error {
		return fn(&Transaction{tx: btx, Database: d})
	})
}

func (d *Database) RTransaction(ctx context.Context, fn func(tx port.Transaction) error) error {
	return d.View(func(btx *bolt.Tx) error {
		return fn(&Transaction{tx: btx, Database: d})
	})
}

func (d *Database) PutDocument(ctx context.Context, doc *model.Document) (string, error) {
	var rev string
	err := d.Transaction(ctx, func(tx port.Transaction) error {
		var err error
		rev, err = tx.PutDocument(ctx, doc)
		return err
	})
	return rev, err
}

func (d *Database) GetDocument(ctx context.Context, docID string) (*model.Document, error) {
	var doc *model.Document
	err := d.RTransaction(ctx, func(tx port.Transaction) error {
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
	err := d.Transaction(ctx, func(tx port.Transaction) error {
		var err error
		doc, err = tx.DeleteDocument(ctx, docID, rev)
		return err

	})
	if err != nil {
		return nil, err
	}

	return doc, nil
}
