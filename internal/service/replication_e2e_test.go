package service_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"github.com/goydb/goydb/pkg/replication"
	"github.com/goydb/goydb/pkg/replicator"
	"github.com/goydb/goydb/pkg/unixhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupE2E(t *testing.T) (source *storage.Storage, target *storage.Storage, cleanup func()) {
	dir1, err := os.MkdirTemp(os.TempDir(), "goydb-e2e-src-*")
	require.NoError(t, err)
	dir2, err := os.MkdirTemp(os.TempDir(), "goydb-e2e-tgt-*")
	require.NoError(t, err)

	s1, err := storage.Open(dir1, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	s2, err := storage.Open(dir2, storage.WithLogger(logger.NewNoLog()))
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

	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
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

	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
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

	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb", Continuous: true}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())

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

	peerA := &replication.LocalDB{Storage: srcStorage, DBName: "dba"}
	peerB := &replication.LocalDB{Storage: tgtStorage, DBName: "dbb"}

	// A -> B
	repDoc1 := &model.ReplicationDoc{Source: "dba", Target: "dbb"}
	r1 := replicator.NewReplicator(peerA, peerB, repDoc1, logger.NewNoLog())
	_, err = r1.Run(ctx)
	require.NoError(t, err)

	// B -> A
	repDoc2 := &model.ReplicationDoc{Source: "dbb", Target: "dba"}
	r2 := replicator.NewReplicator(peerB, peerA, repDoc2, logger.NewNoLog())
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

	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb", CreateTarget: true}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
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

	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
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

	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
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
	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
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

	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())

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

// TestE2E_AttachmentReplication verifies that binary attachments are carried
// from source to target during local-to-local replication.
func TestE2E_AttachmentReplication(t *testing.T) {
	srcStorage, tgtStorage, cleanup := setupE2E(t)
	defer cleanup()

	ctx := context.Background()

	srcDB, err := srcStorage.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = tgtStorage.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Put a document then attach a file to it.
	_, err = srcDB.PutDocument(ctx, &model.Document{
		ID:   "docwithatt",
		Data: map[string]interface{}{"hello": "world"},
	})
	require.NoError(t, err)

	attContent := "binary attachment content for replication test"
	att := &model.Attachment{
		Filename:    "hello.txt",
		ContentType: "text/plain",
		Reader:      io.NopCloser(strings.NewReader(attContent)),
		ExpectedRev: "1-" + strings.Repeat("0", 32), // placeholder; PutAttachment uses the stored rev
	}
	// Fetch the actual rev first.
	srcDoc, err := srcDB.GetDocument(ctx, "docwithatt")
	require.NoError(t, err)
	att.ExpectedRev = srcDoc.Rev
	_, err = srcDB.PutAttachment(ctx, "docwithatt", att)
	require.NoError(t, err)

	// Replicate sourcedb → targetdb.
	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
	result, err := r.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.DocsWritten)

	// The document must exist on the target.
	tgtDB, err := tgtStorage.Database(ctx, "targetdb")
	require.NoError(t, err)
	tgtDoc, err := tgtDB.GetDocument(ctx, "docwithatt")
	require.NoError(t, err)
	require.NotNil(t, tgtDoc)

	// The attachment must be accessible and contain the original content.
	tgtAtt, err := tgtDB.GetAttachment(ctx, "docwithatt", "hello.txt")
	require.NoError(t, err)
	require.NotNil(t, tgtAtt)

	gotData, err := io.ReadAll(tgtAtt.Reader)
	require.NoError(t, err)
	_ = tgtAtt.Reader.Close()
	assert.Equal(t, attContent, string(gotData))
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
	source := &replication.LocalDB{Storage: srcStorage, DBName: "sourcedb"}
	target := &replication.LocalDB{Storage: tgtStorage, DBName: "targetdb"}

	repDoc := &model.ReplicationDoc{Source: "sourcedb", Target: "targetdb"}
	r := replicator.NewReplicator(source, target, repDoc, logger.NewNoLog())
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

// setupE2EChain is setupE2E extended to three independent storages, for
// tests that chain replication across more than one hop (A <-> B <-> C).
func setupE2EChain(t *testing.T) (storA, storB, storC *storage.Storage, cleanup func()) {
	dirA, err := os.MkdirTemp(os.TempDir(), "goydb-e2e-chain-a-*")
	require.NoError(t, err)
	dirB, err := os.MkdirTemp(os.TempDir(), "goydb-e2e-chain-b-*")
	require.NoError(t, err)
	dirC, err := os.MkdirTemp(os.TempDir(), "goydb-e2e-chain-c-*")
	require.NoError(t, err)

	sA, err := storage.Open(dirA, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	sB, err := storage.Open(dirB, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	sC, err := storage.Open(dirC, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	return sA, sB, sC, func() {
		_ = sA.Close()
		_ = sB.Close()
		_ = sC.Close()
		_ = os.RemoveAll(dirA)
		_ = os.RemoveAll(dirB)
		_ = os.RemoveAll(dirC)
	}
}

// waitForDoc polls db until a document with the given id appears, then
// returns it.
func waitForDoc(t *testing.T, db port.Database, id string) *model.Document {
	t.Helper()
	var doc *model.Document
	require.Eventually(t, func() bool {
		d, err := db.GetDocument(context.Background(), id)
		if err != nil || d == nil {
			return false
		}
		doc = d
		return true
	}, 8*time.Second, 200*time.Millisecond, "document %q did not appear", id)
	return doc
}

// waitForDeleted polls db until the document with the given id is a
// tombstone (Deleted == true).
func waitForDeleted(t *testing.T, db port.Database, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		d, err := db.GetDocument(context.Background(), id)
		return err == nil && d != nil && d.Deleted
	}, 8*time.Second, 200*time.Millisecond, "document %q was not deleted", id)
}

// startChainReplication launches the four continuous replicators needed for
// a bidirectional A <-> B <-> C chain (A->B, B->A, B->C, C->B) as goroutines
// bound to repCtx. peerA/peerB/peerC may be any mix of port.ReplicationPeer
// implementations (e.g. LocalDB and RemoteClient).
func startChainReplication(t *testing.T, repCtx context.Context, peerA, peerB, peerC port.ReplicationPeer, nameA, nameB, nameC string) {
	t.Helper()

	legs := []struct {
		source, target         port.ReplicationPeer
		sourceName, targetName string
	}{
		{peerA, peerB, nameA, nameB},
		{peerB, peerA, nameB, nameA},
		{peerB, peerC, nameB, nameC},
		{peerC, peerB, nameC, nameB},
	}
	for _, leg := range legs {
		repDoc := &model.ReplicationDoc{Source: leg.sourceName, Target: leg.targetName, Continuous: true}
		r := replicator.NewReplicator(leg.source, leg.target, repDoc, logger.NewNoLog())
		go func() {
			r.Run(repCtx) // nolint: errcheck
		}()
	}
}

// chainCRUDPropagationTest performs Create/Update/Delete operations at both
// ends of an already-replicating A <-> B <-> C chain and verifies each change
// propagates through the middle hop (B) all the way to the far end.
func chainCRUDPropagationTest(t *testing.T, dbA, dbB, dbC port.Database) {
	t.Helper()
	ctx := context.Background()

	// 1. Create on A must propagate through B to C.
	_, err := dbA.PutDocument(ctx, &model.Document{ID: "from-a", Data: map[string]interface{}{"origin": "A"}})
	require.NoError(t, err)
	waitForDoc(t, dbB, "from-a")
	waitForDoc(t, dbC, "from-a")

	// 2. Create on C must propagate through B to A.
	_, err = dbC.PutDocument(ctx, &model.Document{ID: "from-c", Data: map[string]interface{}{"origin": "C"}})
	require.NoError(t, err)
	waitForDoc(t, dbB, "from-c")
	waitForDoc(t, dbA, "from-c")

	// 3. Update on A must propagate through B to C.
	docA := waitForDoc(t, dbA, "from-a")
	docA.Data["updated"] = "by-a"
	_, err = dbA.PutDocument(ctx, docA)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		d, err := dbB.GetDocument(ctx, "from-a")
		return err == nil && d != nil && d.Data["updated"] == "by-a"
	}, 8*time.Second, 200*time.Millisecond, "update on A did not reach B")
	require.Eventually(t, func() bool {
		d, err := dbC.GetDocument(ctx, "from-a")
		return err == nil && d != nil && d.Data["updated"] == "by-a"
	}, 8*time.Second, 200*time.Millisecond, "update on A did not reach C")

	// 4. Update on C must propagate through B to A.
	docC := waitForDoc(t, dbC, "from-c")
	docC.Data["updated"] = "by-c"
	_, err = dbC.PutDocument(ctx, docC)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		d, err := dbB.GetDocument(ctx, "from-c")
		return err == nil && d != nil && d.Data["updated"] == "by-c"
	}, 8*time.Second, 200*time.Millisecond, "update on C did not reach B")
	require.Eventually(t, func() bool {
		d, err := dbA.GetDocument(ctx, "from-c")
		return err == nil && d != nil && d.Data["updated"] == "by-c"
	}, 8*time.Second, 200*time.Millisecond, "update on C did not reach A")

	// 5. Delete on A must propagate through B to C.
	docA2, err := dbA.GetDocument(ctx, "from-a")
	require.NoError(t, err)
	_, err = dbA.DeleteDocument(ctx, "from-a", docA2.Rev)
	require.NoError(t, err)
	waitForDeleted(t, dbB, "from-a")
	waitForDeleted(t, dbC, "from-a")

	// 6. Delete on C must propagate through B to A.
	docC2, err := dbC.GetDocument(ctx, "from-c")
	require.NoError(t, err)
	_, err = dbC.DeleteDocument(ctx, "from-c", docC2.Rev)
	require.NoError(t, err)
	waitForDeleted(t, dbB, "from-c")
	waitForDeleted(t, dbA, "from-c")
}

// TestE2E_ChainedBidirectionalReplication_CRUDPropagates chains three
// independent databases A <-> B <-> C with continuous, bidirectional
// replication on each link, then performs Create/Update/Delete operations
// at both ends of the chain (A and C) and verifies each change propagates
// through the middle hop (B) all the way to the far end.
func TestE2E_ChainedBidirectionalReplication_CRUDPropagates(t *testing.T) {
	storA, storB, storC, cleanup := setupE2EChain(t)
	defer cleanup()

	ctx := context.Background()

	dbA, err := storA.CreateDatabase(ctx, "dba")
	require.NoError(t, err)
	dbB, err := storB.CreateDatabase(ctx, "dbb")
	require.NoError(t, err)
	dbC, err := storC.CreateDatabase(ctx, "dbc")
	require.NoError(t, err)

	peerA := &replication.LocalDB{Storage: storA, DBName: "dba"}
	peerB := &replication.LocalDB{Storage: storB, DBName: "dbb"}
	peerC := &replication.LocalDB{Storage: storC, DBName: "dbc"}

	repCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	startChainReplication(t, repCtx, peerA, peerB, peerC, "dba", "dbb", "dbc")
	chainCRUDPropagationTest(t, dbA, dbB, dbC)
}

// startRemotePeerServer wraps dbName (living in stor) with a real in-process
// HTTP server speaking the CouchDB-compatible protocol, and returns a
// replication.RemoteClient authenticated against it -- so replication to/from
// this node goes over real HTTP, exactly like replicating against an actual
// remote CouchDB/goydb server, rather than through the in-process LocalDB
// shortcut. Modeled on test/couchdb/reference_test.go's startGoydb.
func startRemotePeerServer(t *testing.T, stor *storage.Storage, dbName string) *replication.RemoteClient {
	t.Helper()
	log := logger.NewNoLog()

	r := mux.NewRouter()
	err := handler.Router{
		Storage:      stor,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: stor, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)
	u.User = url.UserPassword("admin", "secret")
	u.Path = "/" + dbName

	client, err := replication.NewRemoteClient(u.String(), nil)
	require.NoError(t, err)

	return client
}

// startRemoteUnixPeerServer is startRemotePeerServer but reached over a Unix
// domain socket instead of TCP, proving the same replication-over-HTTP path
// works identically regardless of transport.
func startRemoteUnixPeerServer(t *testing.T, stor *storage.Storage, dbName string) *replication.RemoteClient {
	t.Helper()
	log := logger.NewNoLog()

	r := mux.NewRouter()
	err := handler.Router{
		Storage:      stor,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: stor, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	sockPath := filepath.Join(t.TempDir(), "goydb.sock")
	ts := httptest.NewUnstartedServer(r)
	require.NoError(t, ts.Listener.Close())
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)

	rawURL := unixhttp.EncodeURL(sockPath, "/"+dbName)
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	u.User = url.UserPassword("admin", "secret")

	client, err := replication.NewRemoteClient(u.String(), nil)
	require.NoError(t, err)

	return client
}

// TestE2E_ChainedReplication_LocalRemoteLocal_UnixSocket is
// TestE2E_ChainedReplication_LocalRemoteLocal with the middle hop (B) reached
// over a Unix domain socket instead of TCP.
func TestE2E_ChainedReplication_LocalRemoteLocal_UnixSocket(t *testing.T) {
	storA, storB, storC, cleanup := setupE2EChain(t)
	defer cleanup()

	ctx := context.Background()

	dbA, err := storA.CreateDatabase(ctx, "dba")
	require.NoError(t, err)
	dbB, err := storB.CreateDatabase(ctx, "dbb")
	require.NoError(t, err)
	dbC, err := storC.CreateDatabase(ctx, "dbc")
	require.NoError(t, err)

	peerA := &replication.LocalDB{Storage: storA, DBName: "dba"}
	peerB := startRemoteUnixPeerServer(t, storB, "dbb")
	peerC := &replication.LocalDB{Storage: storC, DBName: "dbc"}

	repCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	startChainReplication(t, repCtx, peerA, peerB, peerC, "dba", "dbb", "dbc")
	chainCRUDPropagationTest(t, dbA, dbB, dbC)
}

// TestE2E_ChainedReplication_LocalRemoteLocal is the chain propagation test
// with B reached over real HTTP (the middle hop is "remote") while A and C
// stay local.
func TestE2E_ChainedReplication_LocalRemoteLocal(t *testing.T) {
	storA, storB, storC, cleanup := setupE2EChain(t)
	defer cleanup()

	ctx := context.Background()

	dbA, err := storA.CreateDatabase(ctx, "dba")
	require.NoError(t, err)
	dbB, err := storB.CreateDatabase(ctx, "dbb")
	require.NoError(t, err)
	dbC, err := storC.CreateDatabase(ctx, "dbc")
	require.NoError(t, err)

	peerA := &replication.LocalDB{Storage: storA, DBName: "dba"}
	peerB := startRemotePeerServer(t, storB, "dbb")
	peerC := &replication.LocalDB{Storage: storC, DBName: "dbc"}

	repCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	startChainReplication(t, repCtx, peerA, peerB, peerC, "dba", "dbb", "dbc")
	chainCRUDPropagationTest(t, dbA, dbB, dbC)
}

// TestE2E_ChainedReplication_RemoteLocalLocal is the chain propagation test
// with A reached over real HTTP (one end of the chain is "remote") while B
// and C stay local.
func TestE2E_ChainedReplication_RemoteLocalLocal(t *testing.T) {
	storA, storB, storC, cleanup := setupE2EChain(t)
	defer cleanup()

	ctx := context.Background()

	dbA, err := storA.CreateDatabase(ctx, "dba")
	require.NoError(t, err)
	dbB, err := storB.CreateDatabase(ctx, "dbb")
	require.NoError(t, err)
	dbC, err := storC.CreateDatabase(ctx, "dbc")
	require.NoError(t, err)

	peerA := startRemotePeerServer(t, storA, "dba")
	peerB := &replication.LocalDB{Storage: storB, DBName: "dbb"}
	peerC := &replication.LocalDB{Storage: storC, DBName: "dbc"}

	repCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	startChainReplication(t, repCtx, peerA, peerB, peerC, "dba", "dbb", "dbc")
	chainCRUDPropagationTest(t, dbA, dbB, dbC)
}

// TestE2E_ChainedReplication_LocalLocalRemote is the chain propagation test
// with C reached over real HTTP (the other end of the chain is "remote")
// while A and B stay local.
func TestE2E_ChainedReplication_LocalLocalRemote(t *testing.T) {
	storA, storB, storC, cleanup := setupE2EChain(t)
	defer cleanup()

	ctx := context.Background()

	dbA, err := storA.CreateDatabase(ctx, "dba")
	require.NoError(t, err)
	dbB, err := storB.CreateDatabase(ctx, "dbb")
	require.NoError(t, err)
	dbC, err := storC.CreateDatabase(ctx, "dbc")
	require.NoError(t, err)

	peerA := &replication.LocalDB{Storage: storA, DBName: "dba"}
	peerB := &replication.LocalDB{Storage: storB, DBName: "dbb"}
	peerC := startRemotePeerServer(t, storC, "dbc")

	repCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	startChainReplication(t, repCtx, peerA, peerB, peerC, "dba", "dbb", "dbc")
	chainCRUDPropagationTest(t, dbA, dbB, dbC)
}
