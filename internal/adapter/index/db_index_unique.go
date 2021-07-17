package index

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
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
	cleanKey    func([]byte) string
	mu          sync.RWMutex
}

func NewUniqueIndex(bucketName string, key, value IndexFunc) *UniqueIndex {
	return &UniqueIndex{
		bucketName: []byte(bucketName),
		key:        key,
		value:      value,
	}
}

func (i *UniqueIndex) String() string {
	return fmt.Sprintf("<UniqueIndex name=%q>", string(i.bucketName))
}

func (i *UniqueIndex) Ensure(ctx context.Context, tx port.EngineWriteTransaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	tx.EnsureBucket(i.bucketName)
	return nil
}

func (i *UniqueIndex) Remove(ctx context.Context, tx port.EngineWriteTransaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	tx.DeleteBucket(i.bucketName)
	return nil
}

func (i *UniqueIndex) Stats(ctx context.Context, tx port.EngineReadTransaction) (*model.IndexStats, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return tx.BucketStats(i.bucketName)
}

func (i *UniqueIndex) DocumentStored(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}

	return i.UpdateStored(ctx, tx, []*model.Document{doc})
}

func (i *UniqueIndex) UpdateStored(ctx context.Context, tx port.EngineWriteTransaction, docs []*model.Document) error {
	if len(docs) == 0 {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, doc := range docs {
		k := i.key(doc)
		if k == nil {
			return nil
		}
		v := i.value(doc)
		tx.Put(i.bucketName, k, v)
	}

	return nil
}

func (i *UniqueIndex) DocumentDeleted(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	tx.Delete(i.bucketName, i.key(doc))
	return nil
}

func (i *UniqueIndex) IteratorOptions(ctx context.Context) (*model.IteratorOptions, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	iter := &model.IteratorOptions{
		Skip:        0,
		Limit:       -1,
		SkipDeleted: true,
		StartKey:    nil,
		EndKey:      nil,
		BucketName:  i.bucketName,
		CleanKey:    i.cleanKey,
		KeyFunc:     i.iterKeyFunc,
	}
	return iter, nil
}
