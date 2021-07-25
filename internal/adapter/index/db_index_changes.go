package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const (
	ChangesIndexName             = "_changes"
	ChangesIndexInvalidationName = "_changes:invalidation"
)

type ChangesIndex struct {
	mu sync.RWMutex
}

func NewChangesIndex() *ChangesIndex {
	return &ChangesIndex{}
}

func (i *ChangesIndex) String() string {
	return fmt.Sprintf("<ChangesIndex name=%q>", ChangesIndexName)
}

func (i *ChangesIndex) Ensure(ctx context.Context, tx port.EngineWriteTransaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	tx.EnsureBucket([]byte(ChangesIndexName))
	tx.EnsureBucket([]byte(ChangesIndexInvalidationName))
	return nil
}

func (i *ChangesIndex) Remove(ctx context.Context, tx port.EngineWriteTransaction) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	tx.DeleteBucket([]byte(ChangesIndexName))
	tx.DeleteBucket([]byte(ChangesIndexInvalidationName))
	return nil
}

func (i *ChangesIndex) Stats(ctx context.Context, tx port.EngineReadTransaction) (*model.IndexStats, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	s := tx.BucketStats([]byte(ChangesIndexName))
	si := tx.BucketStats([]byte(ChangesIndexInvalidationName))

	// add size of invalidation bucket as well
	s.Allocated += si.Allocated
	s.Used += si.Used

	return s, nil
}

func (i *ChangesIndex) DocumentStored(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}

	return i.UpdateStored(ctx, tx, []*model.Document{doc})
}

func (i *ChangesIndex) UpdateStored(ctx context.Context, tx port.EngineWriteTransaction, docs []*model.Document) error {
	if len(docs) == 0 {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, doc := range docs {
		tx.PutWithSequence([]byte(ChangesIndexName), nil, []byte(doc.ID), func(_, _ []byte, seq uint64) (newKey []byte, newValue []byte) {
			return uint64ToKey(seq), nil
		})
		// also add invalidation record
		tx.PutWithSequence([]byte(ChangesIndexInvalidationName), []byte(doc.ID), nil, func(_, _ []byte, seq uint64) (newKey []byte, newValue []byte) {
			return nil, uint64ToKey(seq - 1)
		})
	}

	return nil
}

func (i *ChangesIndex) DocumentDeleted(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	realKey, err := tx.Get([]byte(ChangesIndexInvalidationName), []byte(doc.ID))
	if err == port.ErrNotFound {
		return nil // already deleted
	}

	tx.Delete([]byte(ChangesIndexInvalidationName), []byte(doc.ID))
	tx.Delete([]byte(ChangesIndexName), realKey)
	return nil
}

func (i *ChangesIndex) IteratorOptions(ctx context.Context) (*model.IteratorOptions, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	iter := &model.IteratorOptions{
		Skip:        0,
		Limit:       -1,
		SkipDeleted: true,
		StartKey:    nil,
		EndKey:      nil,
		BucketName:  []byte(ChangesIndexName),
		CleanKey:    cleanUint64Key,
		KeyFunc:     byteToUint64Key,
	}
	return iter, nil
}

// LocalSeq will add the local sequence of the document to the document
func LocalSeq(ctx context.Context, tx port.EngineReadTransaction, doc *model.Document) error {
	realKey, err := tx.Get([]byte(ChangesIndexInvalidationName), []byte(doc.ID))
	if err != nil {
		return err
	}
	ui := binary.BigEndian.Uint64(realKey)
	doc.LocalSeq = ui
	return nil
}
