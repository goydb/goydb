//go:build sqlite

package sqlite

import (
	"context"
	"fmt"

	"github.com/goydb/goydb/pkg/port"
)

var _ port.EngineWriteTransaction = (*WriteTransaction)(nil)

type opCode int

const (
	opEnsureBucket opCode = iota
	opDeleteBucket
	opPut
	opPutWithSequence
	opPutWithReusedSequence
	opDelete
)

func (c opCode) String() string {
	switch c {
	case opEnsureBucket:
		return "ensure_bucket"
	case opDeleteBucket:
		return "delete_bucket"
	case opPut:
		return "put"
	case opPutWithSequence:
		return "put_with_sequence"
	case opPutWithReusedSequence:
		return "put_with_reused_sequence"
	case opDelete:
		return "delete"
	default:
		return "unknown"
	}
}

type op struct {
	code       opCode
	bucket     []byte
	k, v       []byte
	keyWithSeq port.KeyWithSeq
}

// WriteTransaction queues every mutation into an op log instead of applying
// it immediately, exactly mirroring bbolt.WriteTransaction: fn runs
// against a plain, non-exclusive read transaction (embedded ReadTransaction)
// so Get/Cursor/Sequence/BucketStats do NOT see this transaction's own
// pending writes, and — critically — so that a WriteTransaction called
// (directly or transitively) from within fn can itself run to completion
// without blocking on a write lock the outer call already holds. Database
// code relies on exactly this nesting (e.g. CreateDatabase's write
// transaction calls BuildIndices, which opens its own nested write
// transaction via AllDocs/Iterator to re-read documents).
//
// Only once fn returns does DB.WriteTransaction check whether the op log is
// non-empty and, if so, acquire the single write-pool connection and replay
// it via Commit — the only phase that is ever mutually exclusive, and the
// point at which deferred foreign key constraints (schema.go) are checked.
type WriteTransaction struct {
	ReadTransaction
	seq   uint64
	opLog []op

	// postCommit collects bucketCache mutations queued by EnsureBucket/
	// DeleteBucket during replay. They are NOT applied here — Commit runs
	// inside the real SQL transaction, which can still be rolled back
	// afterward (a later op, or the final deferred-FK COMMIT itself) — only
	// DB.WriteTransaction applies them, and only after that COMMIT succeeds.
	postCommit []cacheOp
}

// cacheOp is a deferred bucketCache mutation: set bucket->table, or (when
// table == "") delete bucket's entry.
type cacheOp struct {
	bucket string
	table  string
}

func NewWriteTransaction(ex execer, cache *bucketCache) *WriteTransaction {
	return &WriteTransaction{ReadTransaction: ReadTransaction{ex: ex, cache: cache}}
}

func (t *WriteTransaction) EnsureBucket(bucket []byte) {
	t.opLog = append(t.opLog, op{code: opEnsureBucket, bucket: bucket})
}

func (t *WriteTransaction) DeleteBucket(bucket []byte) {
	t.opLog = append(t.opLog, op{code: opDeleteBucket, bucket: bucket})
}

func (t *WriteTransaction) Put(bucket, k, v []byte) {
	t.opLog = append(t.opLog, op{code: opPut, bucket: bucket, k: k, v: v})
}

func (t *WriteTransaction) PutWithSequence(bucket, k, v []byte, fn port.KeyWithSeq) {
	t.opLog = append(t.opLog, op{code: opPutWithSequence, bucket: bucket, k: k, v: v, keyWithSeq: fn})
}

// PutWithReusedSequence reuses t.seq from the immediately preceding
// PutWithSequence op in this transaction's log, exactly mirroring
// bbolt.WriteTransaction's single shared `seq` field semantics —
// callers (e.g. the regular/view and changes indices) pair the two calls
// back-to-back and depend on the reuse. Both the increment and the reuse
// happen at replay time (see Commit), not when the op is queued.
func (t *WriteTransaction) PutWithReusedSequence(bucket, k, v []byte, fn port.KeyWithSeq) {
	t.opLog = append(t.opLog, op{code: opPutWithReusedSequence, bucket: bucket, k: k, v: v, keyWithSeq: fn})
}

func (t *WriteTransaction) Delete(bucket, k []byte) {
	t.opLog = append(t.opLog, op{code: opDelete, bucket: bucket, k: k})
}

// Commit replays the op log against ex (the dedicated write-pool connection
// DB.WriteTransaction placed into a BEGIN IMMEDIATE transaction). The first
// failing op aborts replay; DB.WriteTransaction rolls back the whole write
// transaction in that case, so ops already applied earlier in the log never
// become visible — matching bbolt's all-or-nothing Update semantics.
func (t *WriteTransaction) Commit(ex execer) error {
	ctx := context.Background()
	for _, o := range t.opLog {
		var err error
		switch o.code {
		case opEnsureBucket:
			var table string
			table, err = ensureBucket(ctx, ex, o.bucket)
			if err == nil && table != "" {
				t.postCommit = append(t.postCommit, cacheOp{bucket: string(o.bucket), table: table})
			}
		case opDeleteBucket:
			err = dropBucket(ctx, ex, o.bucket)
			if err == nil {
				t.postCommit = append(t.postCommit, cacheOp{bucket: string(o.bucket)}) // table == "" means delete
			}
		case opPut:
			err = t.putInto(ctx, ex, o.bucket, o.k, o.v)
		case opPutWithSequence, opPutWithReusedSequence:
			if o.code == opPutWithSequence {
				err = ex.QueryRowContext(ctx,
					`INSERT INTO seq (bucket, value) VALUES (?, 1)
					 ON CONFLICT (bucket) DO UPDATE SET value = value + 1
					 RETURNING value`,
					o.bucket,
				).Scan(&t.seq)
			}
			if err == nil {
				nk, nv := o.keyWithSeq(o.k, o.v, t.seq)
				if nk == nil {
					nk = o.k
				}
				if nv == nil {
					nv = o.v
				}
				err = t.putInto(ctx, ex, o.bucket, nk, nv)
			}
		case opDelete:
			err = t.deleteFrom(ctx, ex, o.bucket, o.k)
		}
		if err != nil {
			return fmt.Errorf("sqlite: replay %s on bucket %q: %w", o.code, o.bucket, err)
		}
	}
	return nil
}

// putInto/deleteFrom run during Commit's replay, against the write-pool
// connection mid-transaction — they use resolveBucketReplay (never the
// cache) since that transaction can still roll back after this point and
// the shared cache must never see a mapping that didn't survive to COMMIT.

func (t *WriteTransaction) putInto(ctx context.Context, ex execer, bucket, key, value []byte) error {
	table, err := resolveBucketReplay(ctx, ex, bucket)
	if err != nil {
		return err
	}
	return table.put(ctx, ex, key, value)
}

func (t *WriteTransaction) deleteFrom(ctx context.Context, ex execer, bucket, key []byte) error {
	table, err := resolveBucketReplay(ctx, ex, bucket)
	if err != nil {
		return err
	}
	return table.del(ctx, ex, key)
}
