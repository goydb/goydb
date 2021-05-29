package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) Changes(ctx context.Context, options *model.ChangesOptions) ([]*model.Document, int, error) {
	var pending int
	var docs []*model.Document
	wait := false

start:
	if options.SinceNow() || wait { // wait for new database changes
		observer := d.NewDocObserver(ctx)
		defer observer.Close()
		observer.WaitForDoc(options.Timeout)
	}

	err := d.RTransaction(ctx, func(tx port.Transaction) error {
		index := d.ChangesIndex()
		i, err := index.Iterator(ctx, tx)
		if err != nil {
			return nil
		}
		i.SetLimit(options.Limit)

		if !options.SinceNow() {
			i.SetStartKey([]byte(options.Since))
		}

		for doc := i.First(); i.Continue(); doc = i.Next() {
			docs = append(docs, doc)
		}

		// get number of remaining changes
		pending = i.Remaining()

		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(docs) == 0 && options.Limit != 0 && !wait {
		wait = true
		goto start
	}

	return docs, pending, nil
}
