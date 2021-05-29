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

type UniqueIndex struct {
	bucketName []byte
	key, value IndexFunc
}

func NewUniqueIndex(name string, key, value IndexFunc) port.DocumentIndex {
	return &UniqueIndex{
		bucketName: []byte(name),
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
	panic("not implemented")
}

func (i *UniqueIndex) Stats(ctx context.Context, tx port.Transaction) (*model.IndexStats, error) {
	panic("not implemented")
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

	iter := &Iterator{
		Skip:        0,
		Limit:       -1,
		SkipDeleted: true,
		StartKey:    nil,
		EndKey:      nil,
		tx:          i.tx(tx),
		bucket:      b,
	}
	return iter, nil
}
