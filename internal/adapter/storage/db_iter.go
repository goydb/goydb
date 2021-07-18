package storage

import (
	"bytes"
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

func (d *Database) Iterator(ctx context.Context, ddfn *model.DesignDocFn, fn func(i port.Iterator) error) error {
	return d.Transaction(ctx, func(tx *Transaction) error {
		io := ForDocuments()
		if ddfn != nil {
			io = ForDesignDocFn(ddfn)
		}
		iter := NewIterator(tx, io)
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

	key []byte
	ctx context.Context

	CleanKey func([]byte) string
	KeyFn    func([]byte) []byte

	bucket []byte
	tx     *Transaction
	cursor port.EngineCursor
}

func (i *Iterator) Total() int {
	stats := i.tx.BucketStats(i.bucket)
	return int(stats.Documents)
}

func NewIterator(tx *Transaction, opts ...IteratorOption) *Iterator {
	iter := &Iterator{
		Skip:        0,
		Limit:       -1,
		SkipDeleted: true,
		StartKey:    nil,
		EndKey:      nil,
		tx:          tx,
	}

	for _, opt := range opts {
		opt(iter)
	}

	return iter
}

type IteratorOption func(*Iterator)

func ForDesignDocFn(ddfn *model.DesignDocFn) IteratorOption {
	return func(i *Iterator) {
		i.bucket = ddfn.Bucket()
	}
}

func ForDocuments() IteratorOption {
	return func(i *Iterator) {
		i.bucket = model.DocsBucket
	}
}

func WithOptions(opts *model.IteratorOptions) IteratorOption {
	return func(i *Iterator) {
		i.Skip = opts.Skip
		i.Limit = opts.Limit
		i.StartKey = opts.StartKey
		i.EndKey = opts.EndKey
		i.SkipDeleted = opts.SkipDeleted
		i.SkipDesignDoc = opts.SkipDesignDoc
		i.SkipLocalDoc = opts.SkipLocalDoc
		i.CleanKey = opts.CleanKey
		i.KeyFn = opts.KeyFunc
		i.bucket = opts.BucketName
	}
}

func (i *Iterator) First() *model.Document {
	i.cursor = i.tx.Cursor(i.bucket)

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
		i.cursor = i.tx.Cursor(i.bucket)
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
	if i.KeyFn != nil {
		v = i.KeyFn(v)
	}
	i.StartKey = v
}

func (i *Iterator) SetEndKey(v []byte) {
	if i.KeyFn != nil {
		v = i.KeyFn(v)
	}
	i.EndKey = v
}
func (i *Iterator) unmarshalDoc(k, v []byte, doc *model.Document) {
	bson.Unmarshal(v, doc) // nolint: errcheck

	// provide the document key via the document
	if i.CleanKey != nil {
		doc.Key = i.CleanKey(k)
	} else {
		doc.Key = string(k)
	}
}
