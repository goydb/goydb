//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goydb/goydb/pkg/port"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeTx(t *testing.T, db *DB, fn func(tx port.EngineWriteTransaction)) {
	t.Helper()
	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		fn(tx)
		return nil
	})
	require.NoError(t, err)
}

func putDoc(t *testing.T, db *DB, id, value string) {
	t.Helper()
	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.Put([]byte("docs"), []byte(id), []byte(value))
	})
}

// --- generic bucketTable (well-known "meta" bucket) -------------------------

func TestGeneric_EmptyBucket(t *testing.T) {
	db := openTestDB(t)

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c := tx.Cursor([]byte("meta"))
		k, v := c.First()
		assert.Nil(t, k)
		assert.Nil(t, v)
		return nil
	})
	require.NoError(t, err)
}

func TestGeneric_ForwardAndBackward(t *testing.T) {
	db := openTestDB(t)
	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.Put([]byte("meta"), []byte("a"), []byte("1"))
		tx.Put([]byte("meta"), []byte("b"), []byte("2"))
		tx.Put([]byte("meta"), []byte("c"), []byte("3"))
	})

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c := tx.Cursor([]byte("meta"))

		k, v := c.First()
		assert.Equal(t, "a", string(k))
		assert.Equal(t, "1", string(v))

		k, v = c.Next()
		assert.Equal(t, "b", string(k))
		assert.Equal(t, "2", string(v))

		k, v = c.Next()
		assert.Equal(t, "c", string(k))
		assert.Equal(t, "3", string(v))

		k, _ = c.Next()
		assert.Nil(t, k)

		k, v = c.Last()
		assert.Equal(t, "c", string(k))
		assert.Equal(t, "3", string(v))

		k, v = c.Prev()
		assert.Equal(t, "b", string(k))
		assert.Equal(t, "2", string(v))
		return nil
	})
	require.NoError(t, err)
}

func TestGeneric_Seek(t *testing.T) {
	db := openTestDB(t)
	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.Put([]byte("meta"), []byte("b"), []byte("1"))
		tx.Put([]byte("meta"), []byte("d"), []byte("2"))
	})

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c := tx.Cursor([]byte("meta"))
		k, _ := c.Seek([]byte("c"))
		assert.Equal(t, "d", string(k))
		k, _ = c.Seek([]byte("z"))
		assert.Nil(t, k)
		return nil
	})
	require.NoError(t, err)
}

func TestGeneric_GetMissingReturnsErrNotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		_, err := tx.Get([]byte("meta"), []byte("nope"))
		assert.ErrorIs(t, err, port.ErrNotFound)
		return nil
	})
	require.NoError(t, err)
}

func TestGeneric_DeleteKeepsOthers(t *testing.T) {
	db := openTestDB(t)
	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.Put([]byte("meta"), []byte("a"), []byte("1"))
		tx.Put([]byte("meta"), []byte("b"), []byte("2"))
	})
	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.Delete([]byte("meta"), []byte("a"))
	})

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		_, err := tx.Get([]byte("meta"), []byte("a"))
		assert.ErrorIs(t, err, port.ErrNotFound)
		v, err := tx.Get([]byte("meta"), []byte("b"))
		require.NoError(t, err)
		assert.Equal(t, "2", string(v))
		return nil
	})
	require.NoError(t, err)
}

// --- sequence generation ------------------------------------------------

func TestSequence_ReuseAndIncrement(t *testing.T) {
	db := openTestDB(t)
	var first, reused uint64
	var seqs []uint64

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.PutWithSequence([]byte("meta"), []byte("k1"), []byte("v1"), func(k, v []byte, seq uint64) ([]byte, []byte) {
			first = seq
			seqs = append(seqs, seq)
			return nil, nil
		})
		tx.PutWithReusedSequence([]byte("meta"), []byte("k2"), []byte("v2"), func(k, v []byte, seq uint64) ([]byte, []byte) {
			reused = seq
			return nil, nil
		})
		tx.PutWithSequence([]byte("meta"), []byte("k3"), []byte("v3"), func(k, v []byte, seq uint64) ([]byte, []byte) {
			seqs = append(seqs, seq)
			return nil, nil
		})
	})

	assert.Equal(t, first, reused)
	assert.Equal(t, []uint64{1, 2}, seqs)
}

// --- doc_leaves (decomposed key, FK to documents) ------------------------

func TestDocLeaves_RoundTripsOriginalKeyFormat(t *testing.T) {
	db := openTestDB(t)
	putDoc(t, db, "doc1", `{"_id":"doc1"}`)

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.Put([]byte("doc_leaves"), []byte("doc1\x00rev-a"), []byte("A"))
		tx.Put([]byte("doc_leaves"), []byte("doc1\x00rev-b"), []byte("B"))
	})

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		v, err := tx.Get([]byte("doc_leaves"), []byte("doc1\x00rev-a"))
		require.NoError(t, err)
		assert.Equal(t, "A", string(v))

		c := tx.Cursor([]byte("doc_leaves"))
		prefix := []byte("doc1\x00")
		k, v := c.Seek(prefix)
		require.NotNil(t, k)
		assert.Equal(t, "doc1\x00rev-a", string(k))
		assert.Equal(t, "A", string(v))

		k, v = c.Next()
		require.NotNil(t, k)
		assert.Equal(t, "doc1\x00rev-b", string(k))
		assert.Equal(t, "B", string(v))
		return nil
	})
	require.NoError(t, err)
}

func TestDocLeaves_PrefixScanAcrossMultipleDocs(t *testing.T) {
	db := openTestDB(t)
	putDoc(t, db, "doc1", "{}")
	putDoc(t, db, "doc10", "{}")
	putDoc(t, db, "doc2", "{}")

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.Put([]byte("doc_leaves"), []byte("doc1\x00r1"), []byte("v1"))
		tx.Put([]byte("doc_leaves"), []byte("doc10\x00r1"), []byte("v10"))
		tx.Put([]byte("doc_leaves"), []byte("doc2\x00r1"), []byte("v2"))
	})

	// a prefix scan for "doc1\x00" must only match doc1's leaf, not doc10's,
	// even though "doc10" has "doc1" as a byte-string prefix.
	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c := tx.Cursor([]byte("doc_leaves"))
		prefix := []byte("doc1\x00")
		var revs []string
		for k, _ := c.Seek(prefix); k != nil; k, _ = c.Next() {
			if len(k) < len(prefix) || string(k[:len(prefix)]) != string(prefix) {
				break
			}
			revs = append(revs, string(k[len(prefix):]))
		}
		assert.Equal(t, []string{"r1"}, revs)
		return nil
	})
	require.NoError(t, err)
}

func TestDocLeaves_ForeignKeyRejectsOrphan(t *testing.T) {
	db := openTestDB(t)

	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.Put([]byte("doc_leaves"), []byte("ghost\x00rev-a"), []byte("A"))
		return nil
	})
	assert.Error(t, err, "doc_leaves row referencing a nonexistent document must be rejected")
}

func TestDocLeaves_ForeignKeySucceedsWithinSameTransaction(t *testing.T) {
	db := openTestDB(t)

	// the parent "docs" row and the referencing "doc_leaves" row land in the
	// same write transaction / op log, in that order — this must work even
	// though the FK is only checked at COMMIT (deferred), not after each op.
	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.Put([]byte("docs"), []byte("doc1"), []byte(`{"_id":"doc1"}`))
		tx.Put([]byte("doc_leaves"), []byte("doc1\x00rev-a"), []byte("A"))
		return nil
	})
	require.NoError(t, err)
}

// --- changes / changes_invalidation (FK on the value / key respectively) --

func TestChanges_ForeignKeyRejectsOrphanDocID(t *testing.T) {
	db := openTestDB(t)
	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.PutWithSequence([]byte("_changes"), nil, []byte("ghost"), func(k, v []byte, seq uint64) ([]byte, []byte) {
			b := make([]byte, 8)
			for i := range b {
				b[i] = byte(seq >> (8 * (7 - i)))
			}
			return b, nil
		})
		return nil
	})
	assert.Error(t, err)
}

func TestChangesInvalidation_ForeignKeyRejectsOrphanDocID(t *testing.T) {
	db := openTestDB(t)
	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.Put([]byte("_changes:invalidation"), []byte("ghost"), []byte{0, 0, 0, 0, 0, 0, 0, 1})
		return nil
	})
	assert.Error(t, err)
}

// --- dynamic bucket lifecycle --------------------------------------------

func TestDynamicBucket_EnsureCreatesTable_DeleteDropsIt(t *testing.T) {
	db := openTestDB(t)
	bucket := []byte("views:myddoc:myview")

	// before EnsureBucket, the bucket behaves exactly like an empty one.
	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		_, err := tx.Get(bucket, []byte("k"))
		assert.ErrorIs(t, err, port.ErrNotFound)
		k, _ := tx.Cursor(bucket).First()
		assert.Nil(t, k)
		return nil
	})
	require.NoError(t, err)

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.EnsureBucket(bucket)
		tx.Put(bucket, []byte("k1"), []byte("v1"))
	})

	var tableName string
	err = db.readPool.QueryRow(`SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucket).Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, "idx_views_myddoc_myview", tableName)

	err = db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		v, err := tx.Get(bucket, []byte("k1"))
		require.NoError(t, err)
		assert.Equal(t, "v1", string(v))
		return nil
	})
	require.NoError(t, err)

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.DeleteBucket(bucket)
	})

	err = db.readPool.QueryRow(`SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucket).Scan(&tableName)
	assert.Error(t, err, "bucket_tables mapping should be gone after DeleteBucket")

	err = db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		_, err := tx.Get(bucket, []byte("k1"))
		assert.ErrorIs(t, err, port.ErrNotFound)
		return nil
	})
	require.NoError(t, err)
}

func TestDynamicBucket_MangoInvSuffixGetsForeignKey(t *testing.T) {
	db := openTestDB(t)
	bucket := []byte("mango_indexes:myddoc:myidx:inv")

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.EnsureBucket(bucket)
	})

	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.Put(bucket, []byte("ghost-doc-id"), []byte("some-main-key"))
		return nil
	})
	assert.Error(t, err, "mango :inv buckets are keyed by raw docID and should FK to documents")

	putDoc(t, db, "doc1", "{}")
	err = db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.Put(bucket, []byte("doc1"), []byte("some-main-key"))
		return nil
	})
	assert.NoError(t, err)
}

func TestDynamicBucket_RegularInvalidationSuffixHasNoForeignKey(t *testing.T) {
	db := openTestDB(t)
	bucket := []byte("views:myddoc:myview:invalidation")

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.EnsureBucket(bucket)
	})

	// unlike mango's ":inv", regular-index ":invalidation" buckets are
	// deliberately NOT decomposed/FK'd (wrapped key/value encoding — see
	// buckets.go) — writing an arbitrary key here must succeed even with no
	// matching "documents" row.
	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.Put(bucket, []byte("whatever-wrapped-bytes"), []byte("whatever-value"))
		return nil
	})
	assert.NoError(t, err)
}

// --- pragma / lifecycle ---------------------------------------------------

func TestForeignKeysPragmaEnabled(t *testing.T) {
	db := openTestDB(t)

	var onRead int
	err := db.readPool.QueryRow(`PRAGMA foreign_keys`).Scan(&onRead)
	require.NoError(t, err)
	assert.Equal(t, 1, onRead, "read pool connection should have foreign_keys on")

	var onWritePool int
	err = db.writePool.QueryRow(`PRAGMA foreign_keys`).Scan(&onWritePool)
	require.NoError(t, err)
	assert.Equal(t, 1, onWritePool, "write pool connection should have foreign_keys on")

	err = db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		return nil
	})
	require.NoError(t, err)

	conn, err := db.writePool.Conn(context.Background())
	require.NoError(t, err)
	defer conn.Close()
	var onWriteConn int
	err = conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&onWriteConn)
	require.NoError(t, err)
	assert.Equal(t, 1, onWriteConn, "dedicated write-pool connection should have foreign_keys on")
}

func TestWritePool_SerializesConcurrentWriters(t *testing.T) {
	db := openTestDB(t)

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
				tx.Put([]byte("meta"), []byte(fmt.Sprintf("k%d", i)), []byte(fmt.Sprintf("v%d", i)))
				return nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "writer %d", i)
	}

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		for i := 0; i < n; i++ {
			v, err := tx.Get([]byte("meta"), []byte(fmt.Sprintf("k%d", i)))
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("v%d", i), string(v))
		}
		return nil
	})
	require.NoError(t, err)
}

func TestDeleteEngine_RemovesMainAndSidecarFiles(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "test")
	db, err := Open(base)
	require.NoError(t, err)
	putDoc(t, db, "doc1", "{}")

	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		require.NoError(t, os.WriteFile(base+".sqlite3"+suffix, []byte("x"), 0o644))
	}

	require.NoError(t, db.Delete())

	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_, err := os.Stat(base + ".sqlite3" + suffix)
		assert.True(t, os.IsNotExist(err), "expected %s to be removed", suffix)
	}
}

// --- dynamic bucket name collisions, cache correctness, Stats ------------

func TestDynamicBucket_SanitizedNameCollisionGetsDisambiguated(t *testing.T) {
	db := openTestDB(t)
	// "views:a:b" and "views:a_b" both sanitize to "idx_views_a_b".
	bucketA := []byte("views:a:b")
	bucketB := []byte("views:a_b")

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.EnsureBucket(bucketA)
		tx.Put(bucketA, []byte("k"), []byte("from-a"))
	})
	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.EnsureBucket(bucketB)
		tx.Put(bucketB, []byte("k"), []byte("from-b"))
	})

	var tableA, tableB string
	require.NoError(t, db.readPool.QueryRow(`SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucketA).Scan(&tableA))
	require.NoError(t, db.readPool.QueryRow(`SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucketB).Scan(&tableB))
	assert.NotEqual(t, tableA, tableB, "colliding bucket names must get distinct tables")

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		va, err := tx.Get(bucketA, []byte("k"))
		require.NoError(t, err)
		assert.Equal(t, "from-a", string(va))

		vb, err := tx.Get(bucketB, []byte("k"))
		require.NoError(t, err)
		assert.Equal(t, "from-b", string(vb))
		return nil
	})
	require.NoError(t, err)
}

func TestWriteTransaction_RollbackDoesNotPopulateCache(t *testing.T) {
	db := openTestDB(t)
	bucket := []byte("views:ddoc:willrollback")

	err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
		tx.EnsureBucket(bucket)
		tx.Put(bucket, []byte("k"), []byte("v"))
		// references a nonexistent document -> deferred FK violation at the
		// final COMMIT, rolling back everything in this transaction,
		// including the CREATE TABLE EnsureBucket just replayed (SQLite DDL
		// is transactional).
		tx.Put([]byte("doc_leaves"), []byte("ghost\x00rev-a"), []byte("A"))
		return nil
	})
	assert.Error(t, err, "transaction should fail on the doc_leaves FK violation")

	var tableName string
	err = db.readPool.QueryRow(`SELECT table_name FROM bucket_tables WHERE bucket = ?`, bucket).Scan(&tableName)
	assert.Error(t, err, "bucket_tables mapping must not survive the rollback")

	// If the cache had been populated anyway (the bug this test guards
	// against), this Get would hit a "no such table" SQL error instead of
	// cleanly resolving to ErrNotFound via emptyTable.
	err = db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		_, err := tx.Get(bucket, []byte("k"))
		assert.ErrorIs(t, err, port.ErrNotFound)
		return nil
	})
	require.NoError(t, err)
}

func TestBucketCache_ConsultedNotBypassed(t *testing.T) {
	db := openTestDB(t)
	bucket := []byte("views:ddoc:cached")

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		tx.EnsureBucket(bucket)
		tx.Put(bucket, []byte("k"), []byte("v"))
	})

	// Simulate "the admin row is gone but the in-memory cache still
	// remembers it" — without the cache, resolveBucket would now treat this
	// as an unensured bucket (emptyTable) and Get would return ErrNotFound.
	_, err := db.writePool.Exec(`DELETE FROM bucket_tables WHERE bucket = ?`, bucket)
	require.NoError(t, err)

	err = db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		v, err := tx.Get(bucket, []byte("k"))
		require.NoError(t, err, "cache should still resolve this bucket even though its bucket_tables row is gone")
		assert.Equal(t, "v", string(v))
		return nil
	})
	require.NoError(t, err)
}

func TestStats_ExcludesLocalDocsFromDocCount(t *testing.T) {
	db := openTestDB(t)
	putDoc(t, db, "doc1", "{}")
	putDoc(t, db, "doc2", "{}")
	putDoc(t, db, "_local/foo", "{}")
	putDoc(t, db, "_local/bar", "{}")
	putDoc(t, db, "_local/baz", "{}")

	stats, err := db.Stats()
	require.NoError(t, err)
	assert.EqualValues(t, 2, stats.DocCount)
}

func TestPrefixUpperBound(t *testing.T) {
	assert.Equal(t, []byte("_local0"), prefixUpperBound([]byte("_local/")))
	assert.Nil(t, prefixUpperBound([]byte{0xFF, 0xFF}))
	assert.Equal(t, []byte{0x01}, prefixUpperBound([]byte{0x00}))
}

// --- schema version guard --------------------------------------------------

func TestEnsureSchema_FreshOpenStampsVersion(t *testing.T) {
	db := openTestDB(t)
	var version int
	require.NoError(t, db.writePool.QueryRow(`PRAGMA user_version`).Scan(&version))
	assert.Equal(t, schemaVersion, version)
}

func TestEnsureSchema_RejectsMismatchedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test")

	db, err := Open(path)
	require.NoError(t, err)
	_, err = db.writePool.Exec(`PRAGMA user_version = 999`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Open(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema version")
}

func TestEnsureSchema_RejectsPreVersioningFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test")

	// Simulate the engine's original kv-table schema, which predates
	// PRAGMA user_version stamping entirely (so it reads version 0 — the
	// same as a genuinely new file; table count is what disambiguates
	// them, see ensureSchema's doc comment).
	raw, err := sql.Open("sqlite", "file:"+path+".sqlite3")
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE kv (bucket BLOB, key BLOB, value BLOB, PRIMARY KEY (bucket, key))`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	_, err = Open(path)
	require.Error(t, err, "an old-format database must be refused, not silently treated as empty")
	assert.Contains(t, err.Error(), "schema version")
}

// --- read pool: no deadlock under concurrent nesting -----------------------

func TestReadPool_NoDeadlockUnderConcurrentNesting(t *testing.T) {
	db := openTestDB(t)

	// Mirrors CreateDatabase -> BuildIndices -> Iterator/AllDocs -> another
	// WriteTransaction: a nested WriteTransaction opened from within an
	// outer one's view phase, each holding its own readPool connection for
	// the duration. With the old SetMaxOpenConns(5) cap, enough concurrent
	// pairs of these exhausts the pool with every connection held by an
	// outer frame waiting on an inner one that can never get a connection —
	// a real deadlock, not just contention.
	const n = 10
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			done <- db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
				return db.WriteTransaction(nil, func(inner port.EngineWriteTransaction) error {
					_, _ = inner.Get([]byte("meta"), []byte("whatever"))
					return nil
				})
			})
		}()
	}

	for i := 0; i < n; i++ {
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("deadlock: nested WriteTransaction did not complete in time — readPool exhausted")
		}
	}
}

// --- cursor batching at scale -----------------------------------------------

func TestGenericCursor_BatchingAcrossMultipleBatches(t *testing.T) {
	db := openTestDB(t)
	const n = cursorBatchSize*2 + 37 // multiple full batches plus a partial one

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		for i := 0; i < n; i++ {
			tx.Put([]byte("meta"), []byte(fmt.Sprintf("k%04d", i)), []byte(fmt.Sprintf("v%d", i)))
		}
	})

	var forward []string
	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c := tx.Cursor([]byte("meta"))
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			forward = append(forward, string(k))
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, forward, n)
	assert.True(t, sort.StringsAreSorted(forward))

	var backward []string
	err = db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c := tx.Cursor([]byte("meta"))
		for k, _ := c.Last(); k != nil; k, _ = c.Prev() {
			backward = append(backward, string(k))
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, backward, n)

	reversedForward := make([]string, len(forward))
	for i, k := range forward {
		reversedForward[len(forward)-1-i] = k
	}
	assert.Equal(t, reversedForward, backward)
}

func TestDocLeavesCursor_BatchingAcrossMultipleBatches(t *testing.T) {
	db := openTestDB(t)
	putDoc(t, db, "doc1", "{}")
	const n = cursorBatchSize*2 + 13

	writeTx(t, db, func(tx port.EngineWriteTransaction) {
		for i := 0; i < n; i++ {
			tx.Put([]byte("doc_leaves"), []byte(fmt.Sprintf("doc1\x00rev-%04d", i)), []byte(fmt.Sprintf("v%d", i)))
		}
	})

	var revs []string
	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c := tx.Cursor([]byte("doc_leaves"))
		prefix := []byte("doc1\x00")
		for k, _ := c.Seek(prefix); k != nil; k, _ = c.Next() {
			if len(k) < len(prefix) || string(k[:len(prefix)]) != string(prefix) {
				break
			}
			revs = append(revs, string(k[len(prefix):]))
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, revs, n)
	assert.True(t, sort.StringsAreSorted(revs))
}

// --- Sync --------------------------------------------------------------

func TestSync_DoesNotErrorAndDataSurvives(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.Sync()) // empty database

	for i := 0; i < 200; i++ {
		err := db.WriteTransaction(nil, func(tx port.EngineWriteTransaction) error {
			tx.Put([]byte("meta"), []byte(fmt.Sprintf("k%d", i)), []byte("some filler value for the wal"))
			return nil
		})
		require.NoError(t, err)
	}

	require.NoError(t, db.Sync())

	err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		v, err := tx.Get([]byte("meta"), []byte("k199"))
		require.NoError(t, err)
		assert.Equal(t, "some filler value for the wal", string(v))
		return nil
	})
	require.NoError(t, err)
}
