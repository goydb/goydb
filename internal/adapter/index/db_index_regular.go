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
	cleanKey func([]byte) interface{}

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

	s := tx.BucketStats(i.bucketName)
	si := tx.BucketStats(i.indexInvalidationBucket)

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

		// 2. ignore design documents and local documents when
		//    creating the index
		if doc.IsDesignDoc() || doc.IsLocalDoc() {
			continue
		}

		// 3. add new keys and invalidation records
		keys, values := i.idxFn(ctx, doc)
		for j, key := range keys {
			// enable multi key
			tx.PutWithSequence(i.bucketName, key, values[j], keyWithSeq)

			// store information about the key
			tx.PutWithReusedSequence(i.indexInvalidationBucket, []byte(doc.ID), key, keyWithSeqInv)
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
	c := tx.Cursor(i.indexInvalidationBucket)
	docID := []byte(doc.ID)

	for k, v := c.Seek(docID); k != nil; k, v = c.Next() {
		if len(k) < len(docID) {
			break
		}
		if !bytes.Equal(k[:len(docID)], docID) {
			break // past all keys with this prefix
		}
		// Key format: docID + seq(8) + docIDLen(2).
		// Verify that the stored docID length matches exactly so that
		// e.g. docID "test" does not match keys for "test1".
		if len(k) < 10 || int(binary.BigEndian.Uint16(k[len(k)-2:])) != len(docID) {
			continue
		}

		// document matches, delete key in regular index
		// and mark invalidation key for later deletion
		tx.Delete(i.bucketName, v)
		tx.Delete(i.indexInvalidationBucket, k)
	}

	return nil
}

func (i *RegularIndex) IteratorOptions(ctx context.Context) (*model.IteratorOptions, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var ck func([]byte) interface{}
	// if no func is defined
	if i.cleanKey == nil {
		ck = func(b []byte) interface{} { return simpleCleanKey(b) }
	} else {
		ck = func(b []byte) interface{} {
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

func keyWithSeq(key, _ []byte, seq uint64) ([]byte, []byte) {
	lkey := len(key)
	mk := make([]byte, lkey+8+2)
	copy(mk[:lkey], key)
	binary.BigEndian.PutUint64(mk[lkey:], seq)
	binary.BigEndian.PutUint16(mk[lkey+8:], uint16(lkey))
	return mk, nil
}

func keyWithSeqInv(key, value []byte, seq uint64) ([]byte, []byte) {
	// Build unique invalidation key: docID + seq + docIDLen
	lkey := len(key)
	mk := make([]byte, lkey+8+2)
	copy(mk[:lkey], key)
	binary.BigEndian.PutUint64(mk[lkey:], seq)
	binary.BigEndian.PutUint16(mk[lkey+8:], uint16(lkey))

	// Build value: main index key format (indexKey + seq + indexKeyLen)
	// so RemoveOldKeys can delete the corresponding main index entry
	lval := len(value)
	mv := make([]byte, lval+8+2)
	copy(mv[:lval], value)
	binary.BigEndian.PutUint64(mv[lval:], seq)
	binary.BigEndian.PutUint16(mv[lval+8:], uint16(lval))

	return mk, mv
}

func keyLen(key []byte) uint16 {
	return binary.BigEndian.Uint16(key[len(key)-2:])
}

func simpleCleanKey(k []byte) string {
	return string(k[:keyLen(k)])
}
