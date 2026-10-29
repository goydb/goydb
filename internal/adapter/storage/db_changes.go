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
		// Snapshot the current high-water mark *before* waiting, so once
		// woken we can resume exactly after whatever existed at this
		// moment. We can't derive this from whichever document happens to
		// trigger the wake-up listener below: the *model.Document passed to
		// NotifyDocumentUpdate is the caller's original write-path object,
		// which never has LocalSeq populated (that's only set when a
		// document is read back out through the changes index) -- so
		// doc.LocalSeq is always 0 there, not the document's real sequence.
		sinceSnapshot, err := d.latestChangeSeq()
		if err != nil {
			return nil, 0, err
		}

		wait := make(chan struct{}, 1) // buffered: both timer and listener may send; no close to avoid send-on-closed panic
		t := time.AfterFunc(options.Timeout, func() { wait <- struct{}{} })
		err = d.AddListener(ctx, port.ChangeListenerFunc(func(ctx context.Context, doc *model.Document) error {
			wait <- struct{}{}
			return context.Canceled // only wait for the next document
		}))
		if err != nil {
			return nil, 0, err
		}
		<-wait
		t.Stop()
		options.Since = sinceSnapshot
	}

	err := d.rawTx(func(tx *Transaction) error {
		// Get the changes index bucket
		cursor := tx.Cursor([]byte(index.ChangesIndexName))
		if cursor == nil {
			return nil
		}

		// Determine start position
		var k, v []byte
		if options.SinceNow() || options.Since == "" || options.Since == "0" {
			k, v = cursor.First()
		} else {
			// Parse since as uint64 and seek to it
			since, _ := strconv.ParseUint(options.Since, 10, 64)
			sinceKey := make([]byte, 8)
			binary.BigEndian.PutUint64(sinceKey, since)
			k, v = cursor.Seek(sinceKey)
			// Seek lands on the first key >= sinceKey, not necessarily an
			// exact match -- the entry at exactly `since` only still exists
			// if that document hasn't been updated again since the
			// checkpoint was taken (an update removes the old changes-index
			// entry and inserts a new one at a fresh sequence, see
			// internal/adapter/storage/db_trx.go's PutDocument and
			// internal/adapter/index/db_index_changes.go's DocumentDeleted).
			// Only advance past the landed-on key when it's that exact
			// match; otherwise Seek already landed on the first real change
			// after `since`, and advancing again would silently drop it.
			if k != nil && binary.BigEndian.Uint64(k) == since {
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
	if len(docs) == 0 && options.Limit != 0 && !wait && options.Feed == "longpoll" {
		wait = true
		goto start
	}

	return docs, pending, nil
}

// latestChangeSeq returns the current high-water mark of the changes index,
// i.e. the sequence of the most recent document change, or "" if the
// database has no changes yet.
func (d *Database) latestChangeSeq() (string, error) {
	var seq string
	err := d.rawTx(func(tx *Transaction) error {
		cursor := tx.Cursor([]byte(index.ChangesIndexName))
		if cursor == nil {
			return nil
		}
		if k, _ := cursor.Last(); k != nil {
			seq = strconv.FormatUint(binary.BigEndian.Uint64(k), 10)
		}
		return nil
	})
	return seq, err
}

// seqGreater reports whether a is a strictly greater sequence than b.
// Sequences are decimal-string-encoded uint64s; an empty string is treated
// as 0.
func seqGreater(a, b string) bool {
	if a == "" {
		return false
	}
	av, _ := strconv.ParseUint(a, 10, 64)
	bv, _ := strconv.ParseUint(b, 10, 64)
	return av > bv
}

// WaitForChange blocks until the database has at least one document change
// with a sequence greater than since, then returns the new sequence to
// resume from. If since is "" it returns as soon as there is any change at
// all. It blocks until ctx is cancelled or a qualifying change occurs; ctx
// cancellation returns ctx.Err().
func (d *Database) WaitForChange(ctx context.Context, since string) (string, error) {
	if since == "" {
		since = "0"
	}

	latest, err := d.latestChangeSeq()
	if err != nil {
		return "", err
	}
	if seqGreater(latest, since) {
		return latest, nil
	}

	wait := make(chan struct{}, 1)
	err = d.AddListener(ctx, port.ChangeListenerFunc(func(ctx context.Context, doc *model.Document) error {
		select {
		case wait <- struct{}{}:
		default:
		}
		return context.Canceled // one-shot
	}))
	if err != nil {
		return "", err
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-wait:
	}

	// Recheck rather than trusting the single waking document: multiple
	// changes can coalesce between the wakeup and this point.
	return d.latestChangeSeq()
}
