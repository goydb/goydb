package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"go.etcd.io/bbolt"
)

// RegularIndexFunc gets a document and returns multiple keys
// and values or nil, nil.
type RegularIndexFunc func(ctx context.Context, doc *model.Document) ([][]byte, [][]byte)

var _ port.DocumentIndex = (*RegularIndex)(nil)

var indexInvalidationBucketSuffix = []byte(":invalidation")

// RegularIndex is able to store the same value multiple times,
// does so by adding the document id (which is unique) to the end
// of the document key.
type RegularIndex struct {
	ddfn     *model.DesignDocFn
	idxFn    RegularIndexFunc
	mu       sync.RWMutex
	cleanKey func([]byte) string

	bucketName, indexInvalidationBucket []byte
}

func NewRegularIndex(ddfn *model.DesignDocFn, idxFn RegularIndexFunc) *RegularIndex {
	ri := &RegularIndex{
		ddfn:                    ddfn,
		idxFn:                   idxFn,
		bucketName:              ddfn.Bucket(),
		indexInvalidationBucket: append(ddfn.Bucket(), indexInvalidationBucketSuffix...),
	}
	return ri
}

func (i *RegularIndex) String() string {
	return fmt.Sprintf("<RegularIndex name=%q>", i.ddfn)
}

func (i *RegularIndex) tx(tx port.Transaction) *bbolt.Tx {
	return tx.(*Transaction).tx
}

func (i *RegularIndex) buckets(tx port.Transaction) (*bbolt.Bucket, *bbolt.Bucket, error) {
	// regular bucket (keys >= documents)
	b := i.tx(tx).Bucket(i.bucketName)
	if b == nil {
		return nil, nil, ErrBucketUnavailable
	}

	// invalidation bucket
	bi := i.tx(tx).Bucket(i.indexInvalidationBucket)
	if b == nil {
		return nil, nil, ErrBucketUnavailable
	}

	return b, bi, nil
}

func (i *RegularIndex) Ensure(ctx context.Context, tx port.Transaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	_, err := i.tx(tx).CreateBucketIfNotExists(i.bucketName)
	if err != nil {
		return err
	}
	_, err = i.tx(tx).CreateBucketIfNotExists(i.indexInvalidationBucket)
	return err
}

func (i *RegularIndex) Remove(ctx context.Context, tx port.Transaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	err := i.tx(tx).DeleteBucket(i.bucketName)
	if err != nil {
		return err
	}
	return i.tx(tx).DeleteBucket(i.indexInvalidationBucket)
}

func (i *RegularIndex) Stats(ctx context.Context, tx port.Transaction) (*model.IndexStats, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	b, bi, err := i.buckets(tx)
	if err != nil {
		return nil, err
	}

	s := b.Stats()
	si := bi.Stats()

	return &model.IndexStats{
		Keys:      uint64(s.KeyN),
		Documents: uint64(si.KeyN),
		Used:      uint64(s.BranchInuse + s.LeafInuse + si.BranchInuse + si.LeafInuse),
		Allocated: uint64(s.BranchAlloc + s.LeafAlloc + si.BranchAlloc + si.LeafAlloc),
	}, nil
}

func (i *RegularIndex) DocumentStored(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	return i.UpdateStored(ctx, tx, []*model.Document{doc})
}

func (i *RegularIndex) UpdateStored(ctx context.Context, tx port.Transaction, docs []*model.Document) error {
	if len(docs) == 0 {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, doc := range docs {
		if doc == nil {
			return nil
		}

		b, bi, err := i.buckets(tx)
		if err != nil {
			return err
		}

		// 1. remove all old keys from the index
		err = i.RemoveOldKeys(b, bi, doc)
		if err != nil {
			return err
		}

		// 2. add new keys and invalidation records
		keys, values := i.idxFn(ctx, doc)
		for i, key := range keys {
			// enable multi key
			seq, err := b.NextSequence()
			if err != nil {
				return err
			}
			mk := keyWithSeq(key, seq)
			err = b.Put(mk, values[i])
			if err != nil {
				return err
			}

			// store information about the key
			seq, err = bi.NextSequence()
			if err != nil {
				return err
			}
			err = bi.Put(keyWithSeq([]byte(doc.ID), seq), mk)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *RegularIndex) DocumentDeleted(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	b, bi, err := i.buckets(tx)
	if err != nil {
		return err
	}

	return i.RemoveOldKeys(b, bi, doc)
}

func (i *RegularIndex) RemoveOldKeys(b, bi *bbolt.Bucket, doc *model.Document) error {
	var deletionList [][]byte

	// use the invalidation function to get all keys that are
	// created based on the provided document
	c := bi.Cursor()
	for k, v := c.Seek([]byte(doc.ID)); k != nil; k, v = c.Next() {
		// compare key
		if !bytes.Equal(k[:keyLen(k)], []byte(doc.ID)) {
			break // not the same document
		}

		// document matches, delete key in regular index
		// and mark invalidation key for later deletion
		err := b.Delete(v)
		if err != nil {
			return err
		}
		deletionList = append(deletionList, k)
	}

	// remove all invalidation keys
	for _, k := range deletionList {
		err := bi.Delete(k)
		if err != nil {
			return err
		}
	}

	return nil
}

func (i *RegularIndex) Iterator(ctx context.Context, tx port.Transaction) (port.Iterator, error) {
	i.mu.RLock()
	b := i.tx(tx).Bucket(i.bucketName)
	i.mu.RUnlock()
	if b == nil {
		return nil, ErrBucketUnavailable
	}

	var ck func([]byte) string
	// if no func is defined
	if i.cleanKey == nil {
		ck = simpleCleanKey
	} else {
		ck = func(b []byte) string {
			return i.cleanKey([]byte(simpleCleanKey(b)))
		}
	}

	iter := &Iterator{
		Skip:        0,
		Limit:       -1,
		SkipDeleted: true,
		StartKey:    nil,
		EndKey:      nil,
		tx:          i.tx(tx),
		bucket:      b,
		// Iterator only return the key not the meta data
		cleanKey: ck,
	}

	return iter, nil
}

func keyWithSeq(key []byte, seq uint64) []byte {
	lkey := len(key)
	mk := make([]byte, lkey+8+2)
	copy(mk[:lkey], key)
	binary.BigEndian.PutUint64(mk[lkey:], seq)
	binary.BigEndian.PutUint16(mk[lkey+8:], uint16(lkey))
	return mk
}

func keyLen(key []byte) uint16 {
	return binary.BigEndian.Uint16(key[len(key)-2:])
}

func simpleCleanKey(k []byte) string {
	return string(k[:keyLen(k)])
}
