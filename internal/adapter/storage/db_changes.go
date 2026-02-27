package storage

import (
	"context"
	"encoding/binary"
	"strconv"
	"time"

	"github.com/goydb/goydb/internal/adapter/index"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) Changes(ctx context.Context, options *model.ChangesOptions) ([]*model.Document, int, error) {
	var pending int
	var docs []*model.Document
	wait := false

start:
	if options.SinceNow() || wait { // wait for new database changes
		wait := make(chan struct{}, 1) // buffered: both timer and listener may send; no close to avoid send-on-closed panic
		t := time.AfterFunc(options.Timeout, func() { wait <- struct{}{} })
		err := d.AddListener(ctx, port.ChangeListenerFunc(func(ctx context.Context, doc *model.Document) error {
			options.Since = strconv.FormatInt(int64(doc.LocalSeq-1), 10)
			wait <- struct{}{}
			return context.Canceled // only wait for the next document
		}))
		if err != nil {
			return nil, 0, err
		}
		<-wait
		t.Stop()
	}

	err := d.rawTx(func(tx *Transaction) error {
		// Get the changes index bucket
		cursor := tx.Cursor([]byte(index.ChangesIndexName))
		if cursor == nil {
			return nil
		}

		// Determine start position
		var k, v []byte
		if options.SinceNow() || options.Since == "" {
			k, v = cursor.First()
		} else {
			// Parse since as uint64 and seek to it
			since, _ := strconv.ParseUint(options.Since, 10, 64)
			sinceKey := make([]byte, 8)
			binary.BigEndian.PutUint64(sinceKey, since)
			k, v = cursor.Seek(sinceKey)
			// Move to next since we want changes AFTER this sequence
			if k != nil {
				k, v = cursor.Next()
			}
		}

		// Iterate through changes
		count := 0
		limit := options.Limit
		if limit == 0 {
			limit = 1000 // default
		}

		for k != nil && count < limit {
			// Extract document ID from value
			docID := string(v)

			// Look up the full document
			doc, err := tx.GetDocument(ctx, docID)
			if err != nil || doc == nil {
				// Skip documents that can't be found
				k, v = cursor.Next()
				continue
			}

			docs = append(docs, doc)
			count++
			k, v = cursor.Next()
		}

		// Count remaining changes
		for k != nil {
			pending++
			k, _ = cursor.Next()
		}

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
