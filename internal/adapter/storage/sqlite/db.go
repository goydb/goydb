//go:build sqlite

// Package sqlite implements port.DatabaseEngine on top of SQLite
// (via the pure-Go modernc.org/sqlite driver), as an opt-in alternative to
// the default bbolt engine. Enable it with `go build -tags sqlite`.
//
// Unlike a generic KV store, each well-known bucket gets its own real SQL
// table with a shape suited to it (schema.go), and every dynamic bucket
// (one per persisted view/mango index, plus their invalidation companion
// buckets) gets its own generated table too (buckets.go) — so a database's
// schema is directly inspectable (`.tables`/`.schema` in the sqlite3 CLI)
// rather than being one opaque multiplexed table. See buckets.go's
// bucketTable interface and resolveBucket for the dispatch between bucket
// name and concrete table.
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	_ "modernc.org/sqlite"
)

// schemaVersion is stamped into PRAGMA user_version on a freshly created
// database and checked on every subsequent Open. Bump it whenever schema.go
// changes in a way that isn't purely additive-and-backward-compatible; there
// is no migration support, so a mismatch is refused rather than silently
// misinterpreted (see ensureSchema).
const schemaVersion = 1

// writeAcquireTimeout bounds how long WriteTransaction's replay phase will
// wait for writePool's single connection. writePool only ever has one
// transaction in flight per database (no nesting on the write side, unlike
// reads), so under normal operation this never comes close to firing —
// hitting it means a prior write transaction's replay is genuinely stuck,
// and failing loudly beats hanging the entire write path silently.
const writeAcquireTimeout = 30 * time.Second

var _ port.DatabaseEngine = (*DB)(nil)

// execer is satisfied by both *sql.Tx (used for reads) and *sql.Conn (used
// for writes, see WriteTransaction) so ReadTransaction/WriteTransaction/
// Cursor can be written once against either.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB is a port.DatabaseEngine backed by a single SQLite file in WAL mode,
// accessed through two separate connection pools rather than one shared
// pool plus a mutex:
//
//   - writePool is capped at a single connection (SetMaxOpenConns(1)). That
//     cap IS the serialization — a second concurrent writer's writePool.Conn
//     call simply blocks until the first returns its connection, so there is
//     no separate write-lock field to keep in sync with it.
//   - readPool has NO cap on open connections. A read transaction (including
//     the "view phase" of a WriteTransaction — see WriteTransaction) must
//     never block waiting for a connection another transaction on the same
//     database already holds: WriteTransaction's view phase nests routinely
//     (CreateDatabase -> BuildIndices -> Iterator/AllDocs -> another
//     WriteTransaction, each holding its own readPool connection for the
//     duration), so a hard cap here is a deadlock waiting to happen, not
//     just a throughput knob. SetMaxIdleConns/SetConnMaxIdleTime below still
//     bound its steady-state footprint once load subsides.
//
// Both pools are opened against the identical DSN (same pragmas, including
// foreign_keys, which SQLite requires to be set per-connection). Don't add
// an open-connection cap to readPool or a second write path — that's what
// the single-writer/multi-reader contract depends on.
type DB struct {
	path      string
	readPool  *sql.DB
	writePool *sql.DB
	cache     *bucketCache
}

// Open opens (creating if necessary) the SQLite database for the given base
// path, i.e. path+".sqlite3". With no options, every pool setting matches
// this package's original hardcoded defaults exactly. See Option and
// OptionsFromConfig for what's tunable and why writePool's connection
// *count* (as opposed to its idle timeout) deliberately isn't.
func Open(path string, opts ...Option) (*DB, error) {
	cfg := defaultPoolConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	dsn := "file:" + path + ".sqlite3" +
		"?_pragma=journal_mode(WAL)" +
		// NORMAL is SQLite's own recommended setting for WAL mode: it only
		// fsyncs at checkpoints rather than after every commit, which matters
		// a lot for workloads with many small write transactions (goydb does
		// one per document write). It's safe against application crashes;
		// only an OS crash/power loss can lose the last few WAL commits.
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		// Real foreign keys (schema.go) need this on; every FK is declared
		// DEFERRABLE INITIALLY DEFERRED so it's only checked at the COMMIT
		// ending a write transaction's replay phase (see WriteTransaction).
		"&_pragma=foreign_keys(1)"

	writePool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Not configurable: writePool's connection count IS the single-writer
	// serialization mechanism (see DB's doc comment), not a tuning knob.
	writePool.SetMaxOpenConns(1)
	writePool.SetMaxIdleConns(1)
	writePool.SetConnMaxIdleTime(cfg.writerIdleTimeout)

	readPool, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writePool.Close()
		return nil, err
	}
	readPool.SetMaxOpenConns(cfg.maxOpenReaders) // 0 = unbounded — see DB's doc comment
	readPool.SetMaxIdleConns(cfg.maxIdleReaders)
	readPool.SetConnMaxIdleTime(cfg.readerIdleTimeout)

	if err := ensureSchema(writePool); err != nil {
		_ = writePool.Close()
		_ = readPool.Close()
		return nil, err
	}

	return &DB{
		path:      path,
		readPool:  readPool,
		writePool: writePool,
		cache:     newBucketCache(),
	}, nil
}

// ensureSchema applies schema.go's DDL to a genuinely new file, or verifies
// an existing file's stamped PRAGMA user_version matches schemaVersion.
//
// A table count of 0, not user_version == 0, is what decides "this is a new
// file": a pre-versioning database (e.g. the original kv-table design this
// engine used before per-concept tables) also reads user_version == 0, so
// that alone can't tell a genuinely new file apart from an old, incompatible
// one. Without this check, opening an old-format file would silently create
// today's empty tables next to the orphaned old ones and present what looks
// exactly like an empty database — indistinguishable from data loss.
func ensureSchema(db *sql.DB) error {
	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tableCount); err != nil {
		return fmt.Errorf("sqlite: checking for existing schema: %w", err)
	}
	if tableCount == 0 {
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("sqlite: failed to apply schema: %w", err)
		}
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
			return fmt.Errorf("sqlite: failed to stamp schema version: %w", err)
		}
		return nil
	}

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("sqlite: reading schema version: %w", err)
	}
	if version != schemaVersion {
		return fmt.Errorf(
			"sqlite: database has schema version %d, this build expects %d (no migration support) — refusing to open; this may be a database from an earlier, incompatible goydb release",
			version, schemaVersion)
	}
	return nil
}

func (db *DB) Close() error {
	// Best-effort: fold the WAL back into the main file on a clean shutdown
	// rather than leaving it to whatever implicit behavior pool-closing
	// triggers. Never fails Close() over it — e.g. a lingering reader can
	// prevent a full TRUNCATE checkpoint, which is fine, not an error.
	_, _ = db.writePool.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)

	writeErr := db.writePool.Close()
	readErr := db.readPool.Close()
	if writeErr != nil {
		return writeErr
	}
	return readErr
}

// Delete closes the database and removes its main file and any
// WAL/SHM/journal sidecar files.
func (db *DB) Delete() error {
	if err := db.Close(); err != nil {
		return err
	}
	base := db.path + ".sqlite3"
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(base + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (db *DB) ReadTransaction(fn func(tx port.EngineReadTransaction) error) error {
	tx, err := db.readPool.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(NewReadTransaction(tx, db.cache)); err != nil {
		return err
	}
	return tx.Commit()
}

// WriteTransaction executes fn against a plain, non-exclusive read
// transaction (from readPool) that only queues mutations (see
// WriteTransaction's doc comment in write_trx.go) — mirroring
// bbolt's View-then-conditional-Update design. This matters for two
// reasons: it lets a WriteTransaction called from within fn (directly or
// transitively, e.g. by BuildIndices via AllDocs/Iterator) run to completion
// without deadlocking on the single write-pool connection an outer call
// might already hold, and it keeps that connection — acquired only in the
// replay phase below — held for as little time as possible, which is what
// makes WAL's single-writer/multi-reader concurrency actually pay off
// instead of serializing every write-path call end to end.
func (db *DB) WriteTransaction(logger port.Logger, fn func(tx port.EngineWriteTransaction) error) error {
	tx, err := db.readPool.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	wtx := NewWriteTransaction(tx, db.cache)
	if err := fn(wtx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if len(wtx.opLog) == 0 {
		return nil
	}

	// writePool is capped at one connection, so this blocks until any other
	// concurrent writer returns theirs — that cap is the only serialization
	// this needs. Bounded so a wedged prior writer surfaces as an error
	// instead of hanging this call forever.
	acquireCtx, cancel := context.WithTimeout(context.Background(), writeAcquireTimeout)
	defer cancel()
	conn, err := db.writePool.Conn(acquireCtx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ctx := context.Background()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	if err := wtx.Commit(conn); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}
	// A deferred foreign key violation (schema.go) doesn't surface until
	// COMMIT, not at the INSERT that caused it — unlike an ordinary error,
	// a failed COMMIT leaves the transaction open rather than ending it, so
	// an explicit ROLLBACK is required here too before the connection goes
	// back to the pool, or the next user of this pooled connection would
	// find themselves unexpectedly still inside it.
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}

	// Only now, with the SQL transaction durably committed, is it safe to
	// let EnsureBucket/DeleteBucket's table-name mappings into the shared
	// cache — anything queued here was rolled back along with everything
	// else if any of the above failed.
	for _, op := range wtx.postCommit {
		if op.table == "" {
			db.cache.delete(op.bucket)
		} else {
			db.cache.set(op.bucket, op.table)
		}
	}
	return nil
}

// Compact runs VACUUM, which needs exclusive access to the whole file. Under
// concurrent readers it can return SQLITE_BUSY once the DSN's busy_timeout
// (5s) elapses — that's expected, not a bug: callers should treat a Compact
// failure as safe to retry rather than fatal.
func (db *DB) Compact() error {
	_, err := db.writePool.Exec("VACUUM")
	return err
}

// Sync forces any buffered writes to stable storage immediately, backing
// POST /{db}/_ensure_full_commit. Needed specifically because of
// synchronous=NORMAL (see Open's DSN comment): normal commits aren't
// fsynced until the next WAL checkpoint, so without this there would be no
// way to force full durability on demand. bbolt's Sync is correctly
// a no-op by contrast — every bbolt Update already fsyncs.
func (db *DB) Sync() error {
	_, err := db.writePool.Exec(`PRAGMA wal_checkpoint(FULL)`)
	return err
}

// prefixUpperBound returns the smallest byte string greater than every
// string with the given prefix, for use as an exclusive upper bound in a
// range query (`key >= prefix AND key < upperBound`) — the SQL equivalent
// of bbolt's Seek(prefix)+HasPrefix loop, but usable directly in a WHERE
// clause so SQLite can satisfy it from the primary key index instead of a
// full table scan. Returns nil if prefix is all 0xFF bytes, in which case
// no finite upper bound exists (never happens for any prefix actually used
// in this package, but callers should treat nil as "fall back to scanning").
func prefixUpperBound(prefix []byte) []byte {
	upper := append([]byte(nil), prefix...)
	for i := len(upper) - 1; i >= 0; i-- {
		if upper[i] != 0xFF {
			upper[i]++
			return upper[:i+1]
		}
	}
	return nil
}

func (db *DB) Stats() (stats model.DatabaseStats, err error) {
	fi, err := os.Stat(db.path + ".sqlite3")
	if err != nil {
		return stats, err
	}
	stats.FileSize = uint64(fi.Size())

	if err := db.readPool.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&stats.DocCount); err != nil {
		return stats, err
	}

	var delCount uint64
	if err := db.readPool.QueryRow(`SELECT COUNT(*) FROM deleted_docs`).Scan(&delCount); err != nil {
		return stats, err
	}
	stats.DocDelCount = delCount
	if stats.DocCount >= delCount {
		stats.DocCount -= delCount
	}

	// CouchDB never counts _local/* docs in doc_count. Scoped to the
	// _local/ key range so SQLite can use the documents primary key index
	// instead of scanning every document (see prefixUpperBound).
	prefix := []byte(model.LocalDocPrefix)
	var localCount uint64
	if upper := prefixUpperBound(prefix); upper != nil {
		if err := db.readPool.QueryRow(`SELECT COUNT(*) FROM documents WHERE key >= ? AND key < ?`, prefix, upper).Scan(&localCount); err != nil {
			return stats, err
		}
	} else {
		// Unreachable for the actual "_local/" constant (doesn't end in
		// 0xFF), kept only so a hypothetical future prefix degrades to a
		// correct full scan instead of a silently wrong unbounded count.
		rows, err := db.readPool.Query(`SELECT key FROM documents`)
		if err != nil {
			return stats, err
		}
		defer rows.Close()
		for rows.Next() {
			var key []byte
			if err := rows.Scan(&key); err != nil {
				return stats, err
			}
			if bytes.HasPrefix(key, prefix) {
				localCount++
			}
		}
		if err := rows.Err(); err != nil {
			return stats, err
		}
	}
	if stats.DocCount >= localCount {
		stats.DocCount -= localCount
	} else {
		stats.DocCount = 0
	}

	// Real page accounting, via SQLite's own PRAGMAs: Alloc = total file
	// space reserved for the database, InUse = Alloc minus free pages.
	var pageCount, pageSize, freelistCount uint64
	if err := db.readPool.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return stats, err
	}
	if err := db.readPool.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return stats, err
	}
	if err := db.readPool.QueryRow(`PRAGMA freelist_count`).Scan(&freelistCount); err != nil {
		return stats, err
	}
	stats.Alloc = pageCount * pageSize
	stats.InUse = stats.Alloc - freelistCount*pageSize

	return stats, nil
}
