package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

var ChangesIndexKeyFunc = func(doc *model.Document) []byte {
	return []byte(doc.FormatLocalSeq())
}

var ChangesIndexValueFunc = func(doc *model.Document) []byte {
	out, err := bson.Marshal(&model.Document{
		ID:       doc.ID,
		Rev:      doc.Rev,
		LocalSeq: doc.LocalSeq,
		Deleted:  doc.Deleted,
	})
	if err != nil {
		return nil
	}
	return out
}

func (d *Database) Changes(ctx context.Context, options *port.ChangesOptions) ([]*model.Document, int, error) {
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
		i, err := index.Iter(tx)
		if err != nil {
			return nil
		}
		i.SetLimit(options.Limit)

		if key := options.StartKey(); key != nil {
			i.SetStartKey(key)
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
