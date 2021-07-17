package index

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
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

func (i *RegularIndex) Ensure(ctx context.Context, tx port.EngineWriteTransaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	// regular bucket (keys >= documents)
	tx.EnsureBucket(i.bucketName)

	// invalidation bucket
	tx.EnsureBucket(i.indexInvalidationBucket)
	return nil
}

func (i *RegularIndex) Remove(ctx context.Context, tx port.EngineWriteTransaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	tx.DeleteBucket(i.bucketName)
	tx.DeleteBucket(i.indexInvalidationBucket)
	return nil
}

func (i *RegularIndex) Stats(ctx context.Context, tx port.EngineReadTransaction) (*model.IndexStats, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	s, err := tx.BucketStats(i.bucketName)
	if err != nil {
		return nil, err
	}
	si, err := tx.BucketStats(i.indexInvalidationBucket)
	if err != nil {
		return nil, err
	}

	s.Documents = si.Documents // si holds the real number of documents
	s.Allocated += si.Allocated
	s.Used += si.Used

	return s, nil
}

func (i *RegularIndex) DocumentStored(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	return i.UpdateStored(ctx, tx, []*model.Document{doc})
}

func (i *RegularIndex) UpdateStored(ctx context.Context, tx port.EngineWriteTransaction, docs []*model.Document) error {
	if len(docs) == 0 {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, doc := range docs {
		if doc == nil {
			return nil
		}

		// 1. remove all old keys from the index
		err := i.RemoveOldKeys(tx, doc)
		if err != nil {
			return err
		}

		// 2. add new keys and invalidation records
		keys, values := i.idxFn(ctx, doc)
		for j, key := range keys {
			// enable multi key
			tx.PutWithSequence(i.bucketName, key, values[j], keyWithSeq)

			// store information about the key
			tx.PutWithSequence(i.indexInvalidationBucket, []byte(doc.ID), values[j], keyWithSeq)
		}
	}

	return nil
}

func (i *RegularIndex) DocumentDeleted(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	return i.RemoveOldKeys(tx, doc)
}

func (i *RegularIndex) RemoveOldKeys(tx port.EngineWriteTransaction, doc *model.Document) error {
	// use the invalidation function to get all keys that are
	// created based on the provided document
	c, err := tx.Cursor(i.indexInvalidationBucket)
	if err != nil {
		return err
	}

	for k, v := c.Seek([]byte(doc.ID)); k != nil; k, v = c.Next() {
		// compare key
		if !bytes.Equal(k[:keyLen(k)], []byte(doc.ID)) {
			break // not the same document
		}

		// document matches, delete key in regular index
		// and mark invalidation key for later deletion
		tx.Delete(i.bucketName, v)
		// remove all invalidation keys
		tx.Delete(i.indexInvalidationBucket, k)
	}

	return nil
}

func (i *RegularIndex) IteratorOptions(ctx context.Context) (*model.IteratorOptions, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var ck func([]byte) string
	// if no func is defined
	if i.cleanKey == nil {
		ck = simpleCleanKey
	} else {
		ck = func(b []byte) string {
			return i.cleanKey([]byte(simpleCleanKey(b)))
		}
	}

	iter := &model.IteratorOptions{
		Skip:        0,
		Limit:       -1,
		SkipDeleted: true,
		StartKey:    nil,
		EndKey:      nil,
		BucketName:  i.bucketName,
		// Iterator only return the key not the meta data
		CleanKey: ck,
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
