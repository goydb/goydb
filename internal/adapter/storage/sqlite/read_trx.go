//go:build sqlite

package sqlite

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.EngineReadTransaction = (*ReadTransaction)(nil)

type ReadTransaction struct {
	ex    execer
	cache *bucketCache
}

func NewReadTransaction(ex execer, cache *bucketCache) *ReadTransaction {
	return &ReadTransaction{ex: ex, cache: cache}
}

func (tx *ReadTransaction) BucketStats(bucket []byte) *model.IndexStats {
	ctx := context.Background()
	table, err := resolveBucketCached(ctx, tx.ex, tx.cache, bucket)
	if err != nil {
		return &model.IndexStats{}
	}
	return table.stats(ctx, tx.ex)
}

func (tx *ReadTransaction) Get(bucket, key []byte) ([]byte, error) {
	ctx := context.Background()
	table, err := resolveBucketCached(ctx, tx.ex, tx.cache, bucket)
	if err != nil {
		return nil, err
	}
	return table.get(ctx, tx.ex, key)
}

func (tx *ReadTransaction) Cursor(bucket []byte) port.EngineCursor {
	ctx := context.Background()
	table, err := resolveBucketCached(ctx, tx.ex, tx.cache, bucket)
	if err != nil {
		return &emptyCursor{}
	}
	return table.cursor(tx.ex)
}

func (tx *ReadTransaction) Sequence(bucket []byte) uint64 {
	var seq uint64
	// No row (bucket never had PutWithSequence called) => 0, matching bbolt.
	_ = tx.ex.QueryRowContext(context.Background(),
		`SELECT value FROM seq WHERE bucket = ?`, bucket,
	).Scan(&seq)
	return seq
}
