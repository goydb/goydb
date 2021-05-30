package storage

import (
	"bytes"
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	bolt "go.etcd.io/bbolt"
	"gopkg.in/mgo.v2/bson"
)

func (d *Database) Iterator(ctx context.Context, ddfn *model.DesignDocFn, fn func(i port.Iterator) error) error {
	return d.View(func(tx *bolt.Tx) error {
		iter := newIterator(tx, ddfn)
		if iter == nil {
			return nil
		}
		return fn(iter)
	})
}

type Iterator struct {
	Skip     int
	Limit    int
	StartKey []byte
	EndKey   []byte

	SkipDeleted   bool
	SkipDesignDoc bool
	SkipLocalDoc  bool

	key    []byte
	tx     *bolt.Tx
	bucket *bolt.Bucket
	cursor *bolt.Cursor
	ctx    context.Context

	cleanKey func([]byte) []byte
}

func (i *Iterator) Total() int {
	return i.bucket.Stats().KeyN
}

func newIterator(tx *bolt.Tx, ddfn *model.DesignDocFn) *Iterator {
	var bucket *bolt.Bucket
	if ddfn != nil {
		bucket = tx.Bucket(ddfn.Bucket())
	} else {
		bucket = tx.Bucket(docsBucket)
	}
	if bucket == nil {
		return nil
	}

	return &Iterator{
		Skip:        0,
		Limit:       -1,
		SkipDeleted: true,
		StartKey:    nil,
		EndKey:      nil,
		tx:          tx,
		bucket:      bucket,
	}
}

func (i *Iterator) First() *model.Document {
	i.cursor = i.bucket.Cursor()

	var v []byte
	if i.StartKey != nil {
		i.key, v = i.cursor.Seek(i.StartKey)
	} else {
		i.key, v = i.cursor.First()
	}

	if i.Skip != 0 && i.Continue() {
		for j := 0; j < i.Skip && i.key != nil; j++ {
			i.key, v = i.cursor.Next()
		}
	}

	if v != nil {
		for {
			var doc model.Document
			i.unmarshalDoc(i.key, v, &doc)

			// skip over all deleted documents
			if doc.Deleted {
				i.key, v = i.cursor.Next()
				continue
			}

			return &doc
		}
	}

	return nil
}

func (i *Iterator) Next() *model.Document {
	var v []byte
	var doc model.Document
	found := false

	for i.key, v = i.cursor.Next(); i.Continue(); i.key, v = i.cursor.Next() {
		i.unmarshalDoc(i.key, v, &doc)

		// skip deleted
		if i.SkipDeleted && doc.Deleted {
			continue
		}

		// skip design documents if desired
		if i.SkipDesignDoc && doc.IsDesignDoc() {
			continue
		}

		// skip local documents if desired
		if i.SkipLocalDoc && doc.IsLocalDoc() {
			continue
		}

		// doc found, reduce iter limit
		if i.Limit != -1 {
			i.Limit--
		}
		found = true
		break
	}

	if !found {
		return nil
	}

	return &doc
}

func (i *Iterator) Continue() bool {
	if i.key == nil { // last pair
		return false
	}

	if i.Limit == 0 { // no more limit
		return false
	}

	if i.EndKey == nil {
		return true
	}

	return bytes.Compare(i.key, i.EndKey) <= 0
}

// Remaining returns the remaining documents starting at
// the current position till the end of the range
func (i *Iterator) Remaining() int {
	if i.cursor == nil {
		i.cursor = i.bucket.Cursor()
	}

	var remaining int
	for {
		k, _ := i.cursor.Next()
		if k == nil {
			break
		}
		remaining++
	}
	i.cursor.Seek(i.key)
	return remaining
}

func (i *Iterator) IncLimit() {
	i.Limit++
}

func (i *Iterator) SetSkip(v int) {
	i.Skip = v
}

func (i *Iterator) SetSkipDesignDoc(v bool) {
	i.SkipDesignDoc = v
}

func (i *Iterator) SetSkipLocalDoc(v bool) {
	i.SkipLocalDoc = v
}

func (i *Iterator) SetLimit(v int) {
	i.Limit = v
}

func (i *Iterator) SetStartKey(v []byte) {
	i.StartKey = v
}

func (i *Iterator) SetEndKey(v []byte) {
	i.EndKey = v
}

func (i *Iterator) unmarshalDoc(k, v []byte, doc *model.Document) {
	bson.Unmarshal(v, doc) // nolint: errcheck

	// provide the document key via the document
	if i.cleanKey != nil {
		doc.Key = i.cleanKey(k)
	} else {
		doc.Key = k
	}
}
