package replication_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	adapterreplication "github.com/goydb/goydb/internal/adapter/replication"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/replication"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupE2E(t *testing.T) (source *storage.Storage, target *storage.Storage, cleanup func()) {
	dir1, err := os.MkdirTemp(os.TempDir(), "goydb-e2e-src-*")
	require.NoError(t, err)
	dir2, err := os.MkdirTemp(os.TempDir(), "goydb-e2e-tgt-*")
	require.NoError(t, err)

	s1, err := storage.Open(dir1)
	require.NoError(t, err)
	s2, err := storage.Open(dir2)
	require.NoError(t, err)

	return s1, s2, func() {
		_ = s1.Close()
		_ = s2.Close()
		_ = os.RemoveAll(dir1)
		_ = os.RemoveAll(dir2)
	}
}

func TestE2E_PullReplication_OneShot(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create 10 docs in source
	for i := 0; i < 10; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replication.NewReplicator(source, target, repDoc)
	result, err := r.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10, result.DocsWritten)

	// Verify all docs are in target
	for i := 0; i < 10; i++ {
		doc, err := target.GetDoc(ctx, fmt.Sprintf("doc%d", i), false, nil)
		require.NoError(t, err)
		assert.NotNil(t, doc)
	}

	// Run again -- should transfer 0 docs (checkpoint resume)
	result2, err := r.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result2.DocsWritten)
}

func TestE2E_PullReplication_WithDeletedDocs(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create 5 docs, delete 2
	for i := 0; i < 5; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	// Delete doc0 and doc1
	doc0, err := srcDB.GetDocument(ctx, "doc0")
	require.NoError(t, err)
	_, err = srcDB.DeleteDocument(ctx, "doc0", doc0.Rev)
	require.NoError(t, err)

	doc1, err := srcDB.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	_, err = srcDB.DeleteDocument(ctx, "doc1", doc1.Rev)
	require.NoError(t, err)

	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replication.NewReplicator(source, target, repDoc)
	result, err := r.Run(ctx)
	require.NoError(t, err)
	assert.True(t, result.DocsWritten >= 3, "should have written at least the non-deleted docs")
}

func TestE2E_ContinuousReplication_LiveUpdates(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create initial docs
	for i := 0; i < 3; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb", Continuous: true}
	r := replication.NewReplicator(source, target, repDoc)

	repCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	go func() {
		r.Run(repCtx) // nolint: errcheck
	}()

	// Wait for initial 3 docs to appear in target
	require.Eventually(t, func() bool {
		for i := 0; i < 3; i++ {
			doc, err := target.GetDoc(ctx, fmt.Sprintf("doc%d", i), false, nil)
			if err != nil || doc == nil {
				return false
			}
		}
		return true
	}, 8*time.Second, 200*time.Millisecond)

	// Add more docs
	for i := 3; i < 8; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	// Wait for all 8 docs to appear in target
	require.Eventually(t, func() bool {
		for i := 0; i < 8; i++ {
			doc, err := target.GetDoc(ctx, fmt.Sprintf("doc%d", i), false, nil)
			if err != nil || doc == nil {
				return false
			}
		}
		return true
	}, 8*time.Second, 200*time.Millisecond)

	cancel()
}

func TestE2E_BidirectionalReplication(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	dbA, err := srcStorage.CreateDatabase(ctx, "dba")
	require.NoError(t, err)
	dbB, err := tgtStorage.CreateDatabase(ctx, "dbb")
	require.NoError(t, err)

	// DB A has docs 1-5
	for i := 1; i <= 5; i++ {
		_, err := dbA.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"from": "A"},
		})
		require.NoError(t, err)
	}

	// DB B has docs 6-10
	for i := 6; i <= 10; i++ {
		_, err := dbB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"from": "B"},
		})
		require.NoError(t, err)
	}

	peerA := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "dba"}
	peerB := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "dbb"}

	// A -> B
	repDoc1 := &model.ReplicationDoc{Source: "dba", Target: "dbb"}
	r1 := replication.NewReplicator(peerA, peerB, repDoc1)
	_, err = r1.Run(ctx)
	require.NoError(t, err)

	// B -> A
	repDoc2 := &model.ReplicationDoc{Source: "dbb", Target: "dba"}
	r2 := replication.NewReplicator(peerB, peerA, repDoc2)
	_, err = r2.Run(ctx)
	require.NoError(t, err)

	// Both should have docs 1-10
	for i := 1; i <= 10; i++ {
		docA, err := peerA.GetDoc(ctx, fmt.Sprintf("doc%d", i), false, nil)
		require.NoError(t, err)
		assert.NotNil(t, docA)

		docB, err := peerB.GetDoc(ctx, fmt.Sprintf("doc%d", i), false, nil)
		require.NoError(t, err)
		assert.NotNil(t, docB)
	}
}

func TestE2E_CreateTarget(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb", CreateTarget: true}
	r := replication.NewReplicator(source, target, repDoc)
	result, err := r.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, result.DocsWritten)

	// Target should now exist
	assert.NoError(t, target.Head(ctx))
}

func TestE2E_LargeReplication(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create 500 docs
	for i := 0; i < 500; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%04d", i),
			Data: map[string]interface{}{"index": i},
		})
		require.NoError(t, err)
	}

	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replication.NewReplicator(source, target, repDoc)
	result, err := r.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 500, result.DocsWritten)
}

func TestE2E_ReplicationPreservesRevisions(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create and update a doc multiple times
	_, err = srcDB.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"version": 1},
	})
	require.NoError(t, err)

	doc, err := srcDB.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	doc.Data["version"] = 2
	_, err = srcDB.PutDocument(ctx, doc)
	require.NoError(t, err)

	doc, err = srcDB.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	doc.Data["version"] = 3
	rev3, err := srcDB.PutDocument(ctx, doc)
	require.NoError(t, err)

	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replication.NewReplicator(source, target, repDoc)
	_, err = r.Run(ctx)
	require.NoError(t, err)

	// Target should have same revision
	tgtDoc, err := target.GetDoc(ctx, "doc1", false, nil)
	require.NoError(t, err)
	assert.Equal(t, rev3, tgtDoc.Rev)
}

// TestE2E_Protocol_FauxtonScenario mirrors the exact Fauxton verify-install
// replication check: 4 live docs + 1 deleted tombstone + 1 design doc in source.
// After replication the target's AllDocs must report total == 4 (excludes the
// tombstone and the _local checkpoint doc).
func TestE2E_Protocol_FauxtonScenario(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Step 1: PUT test documents
	for _, id := range []string{"test_doc_1", "test_doc_10", "test_doc_20", "test_doc_30"} {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   id,
			Data: map[string]interface{}{"value": id},
		})
		require.NoError(t, err)
	}

	// Step 2: DELETE test_doc_1 — creates a tombstone (rev 2-xxx, Deleted=true)
	doc1, err := srcDB.GetDocument(ctx, "test_doc_1")
	require.NoError(t, err)
	_, err = srcDB.DeleteDocument(ctx, "test_doc_1", doc1.Rev)
	require.NoError(t, err)

	// Step 3: PUT a design document
	_, err = srcDB.PutDocument(ctx, &model.Document{
		ID:   "_design/view_check",
		Data: map[string]interface{}{"views": map[string]interface{}{}},
	})
	require.NoError(t, err)

	// Step 4: Replicate sourcedb → targetdb
	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replication.NewReplicator(source, target, repDoc)
	_, err = r.Run(ctx)
	require.NoError(t, err)

	// Step 5: Inspect targetdb via AllDocs
	tgtDB, err := tgtStorage.Database(ctx, "targetdb")
	require.NoError(t, err)

	docs, total, err := tgtDB.AllDocs(ctx, port.AllDocsQuery{SkipLocal: true})
	require.NoError(t, err)

	// Expect exactly 4 live documents:
	//   _design/view_check, test_doc_10, test_doc_20, test_doc_30
	// (tombstone test_doc_1 and _local/<repID> checkpoint are excluded)
	assert.Equal(t, 4, total, "total_rows must exclude tombstone and _local checkpoint")
	assert.Equal(t, 4, len(docs), "returned docs must exclude tombstone and _local checkpoint")

	for _, doc := range docs {
		assert.False(t, doc.Deleted, "no returned doc should be a tombstone")
	}
}

// TestE2E_Protocol_IncrementalCheckpoint verifies that a second replication run
// using the same RepID transfers only the documents added since the first run.
func TestE2E_Protocol_IncrementalCheckpoint(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create 5 initial documents
	for i := 0; i < 5; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replication.NewReplicator(source, target, repDoc)

	// First run — should replicate all 5 docs
	result1, err := r.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, result1.DocsWritten)

	// Add 3 more docs to the source
	for i := 5; i < 8; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	// Second run — same replicator (same RepID) must only write the 3 new docs
	result2, err := r.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, result2.DocsWritten)

	// Target must now hold all 8 documents
	tgtDB, err := tgtStorage.Database(ctx, "targetdb")
	require.NoError(t, err)
	_, total, err := tgtDB.AllDocs(ctx, port.AllDocsQuery{SkipLocal: true})
	require.NoError(t, err)
	assert.Equal(t, 8, total)
}

// TestE2E_Protocol_AllDocsTotal_ExcludesLocalAndDeleted explicitly exercises
// Iterator.Total() by verifying that total_rows excludes both deleted tombstones
// and _local/* checkpoint documents after a replication.
func TestE2E_Protocol_AllDocsTotal_ExcludesLocalAndDeleted(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create 4 live documents + 1 that will be deleted
	for i := 0; i < 5; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	// Delete doc0 — creates a tombstone
	doc0, err := srcDB.GetDocument(ctx, "doc0")
	require.NoError(t, err)
	_, err = srcDB.DeleteDocument(ctx, "doc0", doc0.Rev)
	require.NoError(t, err)

	// Replicate — target receives 4 live docs + 1 tombstone + 1 _local checkpoint
	source := &adapterreplication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &adapterreplication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replication.NewReplicator(source, target, repDoc)
	_, err = r.Run(ctx)
	require.NoError(t, err)

	// AllDocs on target must report only 4 (live docs) — not 6
	tgtDB, err := tgtStorage.Database(ctx, "targetdb")
	require.NoError(t, err)

	rows, total, err := tgtDB.AllDocs(ctx, port.AllDocsQuery{SkipLocal: true})
	require.NoError(t, err)

	assert.Equal(t, 4, total, "total must exclude tombstone and _local checkpoint")
	assert.Equal(t, 4, len(rows), "returned rows must exclude tombstone and _local checkpoint")
}
