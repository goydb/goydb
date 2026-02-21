package storage

import (
	"context"
	"encoding/binary"

	"github.com/goydb/goydb/pkg/model"
	"gopkg.in/mgo.v2/bson"
)

const defaultRevsLimit = 1000

// GetRevsLimit returns the per-database revision history limit.
// Returns 1000 (the CouchDB default) when no value has been set.
func (d *Database) GetRevsLimit(ctx context.Context) (int, error) {
	limit := defaultRevsLimit
	_ = d.rawTx(func(tx *Transaction) error {
		data, err := tx.Get(model.MetaBucket, model.RevsLimitKey)
		if err != nil {
			return nil // not set yet; use default
		}
		if len(data) == 8 {
			limit = int(binary.BigEndian.Uint64(data))
		}
		return nil
	})
	return limit, nil
}

// SetRevsLimit persists the per-database revision history limit.
func (d *Database) SetRevsLimit(ctx context.Context, limit int) error {
	return d.rawTx(func(tx *Transaction) error {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(limit))
		tx.Put(model.MetaBucket, model.RevsLimitKey, buf[:])
		return nil
	})
}

// compactDocuments trims RevHistory on every doc in both the docs and
// doc_leaves buckets to the current _revs_limit.  Only entries that actually
// change are rewritten.
func (d *Database) compactDocuments(ctx context.Context) error {
	limit, err := d.GetRevsLimit(ctx)
	if err != nil {
		return err
	}

	return d.rawTx(func(tx *Transaction) error {
		// Trim docs bucket.
		cursor := tx.Cursor(model.DocsBucket)
		for k, v := cursor.Seek([]byte{}); k != nil; k, v = cursor.Next() {
			var doc model.Document
			if err := bson.Unmarshal(v, &doc); err != nil {
				continue
			}
			if len(doc.RevHistory) <= limit {
				continue
			}
			doc.RevHistory = doc.RevHistory[:limit]
			data, err := bson.Marshal(&doc)
			if err != nil {
				continue
			}
			tx.Put(model.DocsBucket, append([]byte{}, k...), data)
		}

		// Trim doc_leaves bucket.
		cursor = tx.Cursor(model.DocLeavesBucket)
		for k, v := cursor.Seek([]byte{}); k != nil; k, v = cursor.Next() {
			var doc model.Document
			if err := bson.Unmarshal(v, &doc); err != nil {
				continue
			}
			if len(doc.RevHistory) <= limit {
				continue
			}
			doc.RevHistory = doc.RevHistory[:limit]
			data, err := bson.Marshal(&doc)
			if err != nil {
				continue
			}
			tx.Put(model.DocLeavesBucket, append([]byte{}, k...), data)
		}

		return nil
	})
}
