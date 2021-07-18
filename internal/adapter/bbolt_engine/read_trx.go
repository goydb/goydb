package bbolt_engine

import (
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"go.etcd.io/bbolt"
)

var _ port.EngineReadTransaction = (*ReadTransaction)(nil)

type ReadTransaction struct {
	tx *bbolt.Tx
}

func NewReadTransaction(tx *bbolt.Tx) *ReadTransaction {
	return &ReadTransaction{
		tx: tx,
	}
}

func (tx *ReadTransaction) BucketStats(bucket []byte) (*model.IndexStats, error) {
	b := tx.tx.Bucket(bucket)
	if b == nil {
		return nil, port.ErrUnknownBucket
	}
	s := b.Stats()

	return &model.IndexStats{
		Keys:      uint64(s.KeyN),
		Documents: uint64(s.KeyN),
		Used:      uint64(s.BranchInuse + s.LeafInuse),
		Allocated: uint64(s.BranchAlloc + s.LeafAlloc),
	}, nil
}

func (tx *ReadTransaction) Get(bucket, key []byte) ([]byte, error) {
	b := tx.tx.Bucket(bucket)
	if b == nil {
		return nil, port.ErrUnknownBucket
	}
	value := b.Get(key)
	if value == nil {
		return value, port.ErrNotFound
	}
	return value, nil
}

func (tx *ReadTransaction) Cursor(bucket []byte) (port.EngineCursor, error) {
	b := tx.tx.Bucket(bucket)
	if b == nil {
		return nil, port.ErrUnknownBucket
	}

	return b.Cursor(), nil
}

func (tx *ReadTransaction) Sequence(bucket []byte) (uint64, error) {
	b := tx.tx.Bucket(bucket)
	if b == nil {
		return 0, port.ErrUnknownBucket
	}

	return b.Sequence(), nil
}
