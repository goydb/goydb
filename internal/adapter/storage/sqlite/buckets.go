//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// bucketTable is the interface every schema "shape" from the bucket→table
// mapping implements. resolveBucket dispatches a bucket name to one of
// these; Get/Put/Delete/Cursor/BucketStats/Sequence in read_trx.go and
// write_trx.go just call straight through to it.
type bucketTable interface {
	get(ctx context.Context, ex execer, key []byte) ([]byte, error)
	put(ctx context.Context, ex execer, key, value []byte) error
	del(ctx context.Context, ex execer, key []byte) error
	cursor(ex execer) port.EngineCursor
	stats(ctx context.Context, ex execer) *model.IndexStats
}

// wellKnownTables maps a bucket name, byte-for-byte, to the real table name
// backing it. Every one of these tables is created unconditionally at Open()
// time (schema.go), so resolving one never needs a bucket_tables lookup or
// the cache.
var wellKnownTables = map[string]string{
	"docs":                  "documents",
	"att_refs":              "att_refs",
	"meta":                  "meta",
	"_internal":             "internal_docs",
	"tasks":                 "tasks",
	"_changes":              "changes",
	"_changes:invalidation": "changes_invalidation",
	"_deleted":              "deleted_docs",
}

const docLeavesBucket = "doc_leaves"

// maxTableNameAttempts bounds registerDynamicTable's collision-retry loop.
// Collisions require two distinct bucket names to sanitize to the same
// identifier (e.g. "views:a:b" and "views:a_b" both -> "idx_views_a_b");
// this many successive collisions is far beyond anything that could happen
// by chance and indicates a real bug, not transient contention.
const maxTableNameAttempts = 50

// resolveBucketCached is the entry point used by genuine reads — ReadTransaction
// (read_trx.go) and, transitively, the view phase of a WriteTransaction (both
// run against an already-committed, consistent snapshot) — so it's safe to
// both consult and populate the shared bucketCache. It never creates
// anything; a dynamic bucket that hasn't been through EnsureBucket yet
// resolves to emptyTable{}, matching bbolt's behavior of a nil *bbolt.Bucket
// for a not-yet-created bucket (zero stats, ErrNotFound, an empty cursor —
// see read_trx.go).
func resolveBucketCached(ctx context.Context, ex execer, cache *bucketCache, bucket []byte) (bucketTable, error) {
	return resolveBucketImpl(ctx, ex, cache, bucket)
}

// resolveBucketReplay is the entry point used only from within
// WriteTransaction.Commit's replay (write_trx.go's putInto/deleteFrom): it
// never touches the shared cache. bucket_tables rows inserted earlier in
// that same in-flight transaction ARE visible to its query (same SQL
// transaction), but the transaction can still roll back afterward (a later
// op, or the final deferred-FK COMMIT) — populating the real shared cache
// from inside replay would let a rolled-back bucket's mapping leak into it.
// Only DB.WriteTransaction's postCommit, applied after COMMIT actually
// succeeds, may add a dynamic bucket's mapping to the shared cache.
func resolveBucketReplay(ctx context.Context, ex execer, bucket []byte) (bucketTable, error) {
	return resolveBucketImpl(ctx, ex, nil, bucket)
}

// resolveBucketImpl is the shared lookup behind resolveBucketCached/Replay.
// cache == nil means "skip the cache entirely" (see resolveBucketReplay) —
// callers should go through one of the two named wrappers above rather than
// call this directly, so the cache-skipping behavior is never silently
// implied by an unexplained nil at the call site.
func resolveBucketImpl(ctx context.Context, ex execer, cache *bucketCache, bucket []byte) (bucketTable, error) {
	name := string(bucket)
	if name == docLeavesBucket {
		return docLeavesTable{}, nil
	}
	if table, ok := wellKnownTables[name]; ok {
		return genericTable{name: table}, nil
	}

	if cache != nil {
		if table, ok := cache.get(name); ok {
			return genericTable{name: table}, nil
		}
	}

	var table string
	err := ex.QueryRowContext(ctx, `SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucket).Scan(&table)
	if errors.Is(err, sql.ErrNoRows) {
		return emptyTable{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: resolve bucket %q: %w", name, err)
	}
	if cache != nil {
		cache.set(name, table)
	}
	return genericTable{name: table}, nil
}

// ensureBucket is the write-path handler for EngineWriteTransaction's
// EnsureBucket, invoked during write-transaction replay (write_trx.go). For
// well-known buckets it's a no-op (already created at Open()). For a
// dynamic bucket (one per persisted view/mango index, plus their
// ":invalidation"/":inv" companion buckets) it registers a generated table
// name in bucket_tables and creates the table, if not already done.
//
// It returns the resolved table name (whether newly created or already
// existing) so the caller can populate the cache — but only once the
// enclosing SQL transaction actually commits (see WriteTransaction.Commit
// and DB.WriteTransaction); the cache must never see a table that this
// transaction could still roll back. Returns "" for well-known/doc_leaves
// buckets, which never go in the cache.
func ensureBucket(ctx context.Context, ex execer, bucket []byte) (table string, err error) {
	name := string(bucket)
	if name == docLeavesBucket {
		return "", nil
	}
	if _, ok := wellKnownTables[name]; ok {
		return "", nil
	}

	var existing string
	err = ex.QueryRowContext(ctx, `SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucket).Scan(&existing)
	if err == nil {
		return existing, nil // already created (e.g. a prior process run)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("sqlite: ensure bucket %q: look up table: %w", name, err)
	}

	table, err = registerDynamicTable(ctx, ex, bucket, name)
	if err != nil {
		return "", fmt.Errorf("sqlite: ensure bucket %q: %w", name, err)
	}

	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (key BLOB PRIMARY KEY, value BLOB) WITHOUT ROWID`, quoteIdent(table))
	if strings.HasSuffix(name, ":inv") {
		// MangoIndex's ":inv" bucket is confirmed keyed by raw docID, value =
		// the exact main-bucket key bytes (db_index_mango.go) — safe to FK the
		// key. RegularIndex's ":invalidation" bucket is NOT this simple (see
		// db_index_regular.go) and deliberately stays un-decomposed/un-FK'd,
		// which is why this checks the "mango" spelling specifically.
		ddl = fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (key BLOB PRIMARY KEY REFERENCES documents(key) DEFERRABLE INITIALLY DEFERRED, value BLOB NOT NULL) WITHOUT ROWID`,
			quoteIdent(table))
	}
	if _, err := ex.ExecContext(ctx, ddl); err != nil {
		return "", fmt.Errorf("sqlite: ensure bucket %q: create table %q: %w", name, table, err)
	}
	return table, nil
}

// dropBucket is the write-path handler for DeleteBucket. Only dynamic
// buckets are ever removed in practice (an index/view's Remove() call) —
// well-known buckets have no bucket_tables entry and this is a no-op for
// them, which is fine since nothing ever calls DeleteBucket on one.
func dropBucket(ctx context.Context, ex execer, bucket []byte) error {
	name := string(bucket)
	var table string
	err := ex.QueryRowContext(ctx, `SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucket).Scan(&table)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sqlite: drop bucket %q: look up table: %w", name, err)
	}
	if _, err := ex.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, quoteIdent(table))); err != nil {
		return fmt.Errorf("sqlite: drop bucket %q: drop table %q: %w", name, table, err)
	}
	if _, err := ex.ExecContext(ctx, `DELETE FROM bucket_tables WHERE bucket = ?`, bucket); err != nil {
		return fmt.Errorf("sqlite: drop bucket %q: remove mapping: %w", name, err)
	}
	return nil
}

// registerDynamicTable picks a safe, readable SQL table name for a dynamic
// bucket and records the mapping. Collisions after sanitizing are
// vanishingly unlikely (bucket names are "type:ddoc:fn[:suffix]") but are
// handled defensively via the UNIQUE constraint on bucket_tables.table_name
// — detected by the driver's structured error code (not by matching the
// error string, which could change across modernc.org/sqlite releases) and
// bounded to maxTableNameAttempts so a non-collision error can't spin
// forever.
func registerDynamicTable(ctx context.Context, ex execer, bucket []byte, name string) (string, error) {
	base := "idx_" + sanitizeIdent(name)
	table := base
	for attempt := 0; attempt < maxTableNameAttempts; attempt++ {
		if attempt > 0 {
			table = fmt.Sprintf("%s_%d", base, attempt)
		}
		_, err := ex.ExecContext(ctx, `INSERT INTO bucket_tables (bucket, table_name) VALUES (?, ?)`, bucket, table)
		if err == nil {
			return table, nil
		}
		if !isUniqueConstraintViolation(err) {
			return "", err
		}
		// table_name collision: loop and try the next suffix.
	}
	return "", fmt.Errorf("could not generate a unique table name for bucket %q after %d attempts", name, maxTableNameAttempts)
}

// isUniqueConstraintViolation reports whether err is a SQLite UNIQUE
// constraint violation, via the driver's structured error code rather than
// string-matching (modernc.org/sqlite enables extended result codes, so
// Error.Code() distinguishes SQLITE_CONSTRAINT_UNIQUE from other constraint
// violations like NOT NULL or foreign key failures).
func isUniqueConstraintViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE
}

// emptyTable is resolveBucketCached/Replay's answer for a dynamic bucket that has never
// been through EnsureBucket — it behaves exactly like bbolt's nil
// *bbolt.Bucket: Get is "not found", Cursor is empty, BucketStats is zero.
// put/del are never called on it in practice (EnsureBucket always precedes
// Put/Delete for a given bucket within the same write transaction, mirroring
// bbolt's own "no bucket" error path), so they return a descriptive
// error rather than silently doing nothing.
type emptyTable struct{}

func (emptyTable) get(ctx context.Context, ex execer, key []byte) ([]byte, error) {
	return nil, port.ErrNotFound
}

func (emptyTable) put(ctx context.Context, ex execer, key, value []byte) error {
	return fmt.Errorf("sqlite: put to unensured bucket")
}

func (emptyTable) del(ctx context.Context, ex execer, key []byte) error {
	return nil
}

func (emptyTable) cursor(ex execer) port.EngineCursor {
	return &emptyCursor{}
}

func (emptyTable) stats(ctx context.Context, ex execer) *model.IndexStats {
	return &model.IndexStats{}
}

type emptyCursor struct{}

func (emptyCursor) First() (key, value []byte)           { return nil, nil }
func (emptyCursor) Last() (key, value []byte)            { return nil, nil }
func (emptyCursor) Next() (key, value []byte)            { return nil, nil }
func (emptyCursor) Prev() (key, value []byte)            { return nil, nil }
func (emptyCursor) Seek(seek []byte) (key, value []byte) { return nil, nil }
