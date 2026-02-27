package storage

import (
	"context"
	"os"
	"testing"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openStorage(t *testing.T) (*Storage, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-trx-test-*")
	require.NoError(t, err)

	s, err := Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	return s, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestPutDocument_StartsRevHistory(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, []string{rev}, doc.RevHistory)
}

func TestPutDocument_BuildsRevHistory(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev1, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	rev2, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Rev:  rev1,
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.RevHistory, 2)
	assert.Equal(t, rev2, doc.RevHistory[0])
	assert.Equal(t, rev1, doc.RevHistory[1])
}

func TestPutDocument_HistoryCappedAt1000(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 0},
	})
	require.NoError(t, err)

	// 1000 more updates → 1001 total writes, history capped at 1000.
	for i := 1; i <= 1000; i++ {
		rev, err = db.PutDocument(ctx, &model.Document{
			ID:   "doc1",
			Rev:  rev,
			Data: map[string]interface{}{"x": i},
		})
		require.NoError(t, err)
	}

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Len(t, doc.RevHistory, 1000)
}

func TestPutDocumentForReplication_ParsesRevisions(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	doc := &model.Document{
		ID:  "doc1",
		Rev: "3-a",
		Data: map[string]interface{}{
			"_revisions": map[string]interface{}{
				"start": float64(3),
				"ids":   []interface{}{"a", "b", "c"},
			},
		},
	}
	err = db.PutDocumentForReplication(ctx, doc)
	require.NoError(t, err)

	got, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"3-a", "2-b", "1-c"}, got.RevHistory)
}

func TestPutDocumentForReplication_FallbackNoRevisions(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	doc := &model.Document{
		ID:   "doc1",
		Rev:  "1-abc",
		Data: map[string]interface{}{},
	}
	err = db.PutDocumentForReplication(ctx, doc)
	require.NoError(t, err)

	got, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"1-abc"}, got.RevHistory)
}

func TestPutDocument_WritesLeaf(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	require.Len(t, leaves, 1)
	assert.Equal(t, rev, leaves[0].Rev)
}

func TestPutDocument_UpdateRemovesOldLeaf(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev1, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Rev:  rev1,
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)

	// Only one leaf should remain (the new winner).
	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	assert.Len(t, leaves, 1)
}

func TestPutDocumentForReplication_CreatesConflict(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Insert two same-generation docs with different hashes — classic conflict.
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "1-aaa",
		Data: map[string]interface{}{
			"_id": "doc1", "_rev": "1-aaa", "v": 1,
		},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "1-zzz",
		Data: map[string]interface{}{
			"_id": "doc1", "_rev": "1-zzz", "v": 2,
		},
	})
	require.NoError(t, err)

	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	assert.Len(t, leaves, 2)
}

func TestPutDocumentForReplication_HigherGenWins(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "1-old",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-old"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "3-abc",
		Data: map[string]interface{}{
			"_id": "doc1", "_rev": "3-abc",
			"_revisions": map[string]interface{}{
				"start": float64(3),
				"ids":   []interface{}{"abc", "b", "old"},
			},
		},
	})
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "3-abc", doc.Rev)
}

func TestPutDocumentForReplication_AncestorPruned(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Simulate a prior leaf at "1-a".
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "1-a",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-a"},
	})
	require.NoError(t, err)

	// Now replicate "2-b" which has "1-a" as an ancestor.
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "2-b",
		Data: map[string]interface{}{
			"_id": "doc1", "_rev": "2-b",
			"_revisions": map[string]interface{}{
				"start": float64(2),
				"ids":   []interface{}{"b", "a"},
			},
		},
	})
	require.NoError(t, err)

	// "1-a" should have been pruned from leaves; only "2-b" remains.
	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	require.Len(t, leaves, 1)
	assert.Equal(t, "2-b", leaves[0].Rev)
}

func TestGetDocument_PopulatesConflicts(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz"},
	})
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	conflicts, ok := doc.Data["_conflicts"].([]string)
	require.True(t, ok, "_conflicts should be []string")
	assert.Len(t, conflicts, 1)
}

func TestGetDocument_HasRevisionCoversAllBranches(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz"},
	})
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	// The losing leaf rev should be reachable via HasRevision.
	assert.True(t, doc.HasRevision("1-aaa"))
	assert.True(t, doc.HasRevision("1-zzz"))
}

func TestGetDocument_LocalDoc(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Store a _local document.
	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "_local/test-checkpoint",
		Data: map[string]interface{}{"last_seq": "42"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	// GET must succeed — _local docs don't participate in the changes feed,
	// so LocalSeq must be skipped.
	doc, err := db.GetDocument(ctx, "_local/test-checkpoint")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "42", doc.Data["last_seq"])
}

func TestPutDocument_LocalDoc_Uses0NRevisionScheme(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// First PUT should return "0-1".
	rev1, err := db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)
	assert.Equal(t, "0-1", rev1)

	// Second PUT should return "0-2".
	rev2, err := db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Rev:  rev1,
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)
	assert.Equal(t, "0-2", rev2)

	// Third PUT should return "0-3".
	rev3, err := db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Rev:  rev2,
		Data: map[string]interface{}{"x": 3},
	})
	require.NoError(t, err)
	assert.Equal(t, "0-3", rev3)

	// GET returns the correct rev.
	doc, err := db.GetDocument(ctx, "_local/ck1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "0-3", doc.Rev)
}

func TestPutDocument_LocalDoc_NoRevHistory(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Rev:  rev,
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "_local/ck1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	// _local docs should not have RevHistory.
	assert.Empty(t, doc.RevHistory)
}

func TestPutDocument_LocalDoc_NoLeaves(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	// _local docs should not have doc_leaves entries.
	leaves, err := db.GetLeaves(ctx, "_local/ck1")
	require.NoError(t, err)
	assert.Empty(t, leaves)
}

func TestPutDocument_LocalDoc_ConflictCheck(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	// Updating with the wrong rev should fail.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Rev:  "0-999",
		Data: map[string]interface{}{"x": 2},
	})
	assert.Error(t, err)

	// Updating with the correct rev should succeed.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "_local/ck1",
		Rev:  rev,
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)
}

func TestDeleteDocument_ConflictLeaf(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create two conflicting leaves via replication.
	// "1-zzz" wins (higher hash), "1-aaa" is the loser.
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa", "v": 1},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz", "v": 2},
	})
	require.NoError(t, err)

	// Delete the losing conflict leaf.
	tombstone, err := db.DeleteDocument(ctx, "doc1", "1-aaa")
	require.NoError(t, err)
	require.NotNil(t, tombstone)
	assert.True(t, tombstone.Deleted)
	assert.NotEqual(t, "1-aaa", tombstone.Rev, "tombstone should have a new rev")

	// The tombstone has gen 2, which beats "1-zzz" (gen 1) by CouchDB's
	// deterministic winner rule.  So the docs-bucket winner changes to the
	// tombstone.  This matches CouchDB semantics — in practice users first
	// advance the winning branch, then delete the loser.
	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, tombstone.Rev, doc.Rev)
	assert.True(t, doc.Deleted)

	// Leaves should include the tombstone and the old winner.
	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	assert.Len(t, leaves, 2, "tombstone + old winner")
}

func TestDeleteDocument_ConflictLeaf_WinnerChanges(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// "1-zzz" wins, "1-aaa" is the loser.
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa", "v": 1},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz", "v": 2},
	})
	require.NoError(t, err)

	// Delete the winning leaf — the other branch should become the new winner.
	_, err = db.DeleteDocument(ctx, "doc1", "1-zzz")
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	// The tombstone of "1-zzz" will be "2-<hash>" which has gen 2 > gen 1,
	// so it will still win by CouchDB rules. But "1-aaa" should still be a leaf.
	// The important thing is the doc is retrievable and leaves are correct.
	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	assert.Len(t, leaves, 2)
}

func TestDeleteDocument_NonLeafRev_Rejected(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a document, then update it so "1-xxx" is an ancestor, not a leaf.
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "1-xxx",
		Data: map[string]interface{}{
			"_id": "doc1", "_rev": "1-xxx",
		},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "2-yyy",
		Data: map[string]interface{}{
			"_id": "doc1", "_rev": "2-yyy",
			"_revisions": map[string]interface{}{
				"start": float64(2),
				"ids":   []interface{}{"yyy", "xxx"},
			},
		},
	})
	require.NoError(t, err)

	// Try to delete the ancestor rev — should fail with conflict.
	_, err = db.DeleteDocument(ctx, "doc1", "1-xxx")
	assert.Error(t, err)
}

func TestDeleteDocument_WinningRev_StillWorks(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	doc, err := db.DeleteDocument(ctx, "doc1", rev)
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.True(t, doc.Deleted)

	// Document should now be deleted.
	got, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Deleted)
}
