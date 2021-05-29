package storage

import (
	"context"
	"errors"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"go.etcd.io/bbolt"
)

var _ port.DocumentIndex = (*UniqueIndex)(nil)

var ErrBucketUnavailable = errors.New("bucket unavailable")

type IndexFunc func(doc *model.Document) []byte

type IterKeyFunc func(k []byte) []byte

// UniqueIndex base class for all indices that are based on a bucket
// and work synchronously.
type UniqueIndex struct {
	bucketName  []byte
	key, value  IndexFunc
	iterKeyFunc IterKeyFunc
}

func NewUniqueIndex(bucketName string, key, value IndexFunc) port.DocumentIndex {
	return &UniqueIndex{
		bucketName: []byte(bucketName),
		key:        key,
		value:      value,
	}
}

func (i *UniqueIndex) tx(tx port.Transaction) *bbolt.Tx {
	return tx.(*Transaction).tx
}

func (i *UniqueIndex) Ensure(ctx context.Context, tx port.Transaction) error {
	_, err := i.tx(tx).CreateBucketIfNotExists(i.bucketName)
	return err
}

func (i *UniqueIndex) Rebuild(ctx context.Context, tx port.Transaction) error {
	panic("not implemented")
}

func (i *UniqueIndex) Remove(ctx context.Context, tx port.Transaction) error {
	return i.tx(tx).DeleteBucket(i.bucketName)
}

func (i *UniqueIndex) Stats(ctx context.Context, tx port.Transaction) (*model.IndexStats, error) {
	b := i.tx(tx).Bucket(i.bucketName)
	if b == nil {
		return nil, ErrBucketUnavailable
	}
	s := b.Stats()

	return &model.IndexStats{
		Documents: uint64(s.KeyN),
		Used:      uint64(s.BranchInuse) + uint64(s.LeafInuse),
		Allocated: uint64(s.BranchAlloc) + uint64(s.LeafAlloc),
	}, nil
}

func (i *UniqueIndex) DocumentStored(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}
	b := i.tx(tx).Bucket(i.bucketName)
	if b == nil {
		return ErrBucketUnavailable
	}
	k := i.key(doc)
	if k == nil {
		return nil
	}
	v := i.value(doc)
	return b.Put(k, v)
}

func (i *UniqueIndex) DocumentDeleted(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}
	b := i.tx(tx).Bucket(i.bucketName)
	if b == nil {
		return ErrBucketUnavailable
	}
	k := i.key(doc)
	if k == nil {
		return nil
	}
	return b.Delete(k)
}

func (i *UniqueIndex) Iterator(ctx context.Context, tx port.Transaction) (port.Iterator, error) {
	b := i.tx(tx).Bucket(i.bucketName)
	if b == nil {
		return nil, ErrBucketUnavailable
	}

	iter := &iteratorWithKeyFunc{
		Iterator: Iterator{
			Skip:        0,
			Limit:       -1,
			SkipDeleted: true,
			StartKey:    nil,
			EndKey:      nil,
			tx:          i.tx(tx),
			bucket:      b,
		},
		keyFn: i.iterKeyFunc,
	}
	return iter, nil
}

type iteratorWithKeyFunc struct {
	Iterator
	keyFn IterKeyFunc
}

func (i *iteratorWithKeyFunc) SetStartKey(v []byte) {
	if i.keyFn != nil {
		v = i.keyFn(v)
	}
	i.StartKey = v
}

func (i *iteratorWithKeyFunc) SetEndKey(v []byte) {
	if i.keyFn != nil {
		v = i.keyFn(v)
	}
	i.EndKey = v
}