package replication

import (
	"context"
	"fmt"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Phase 1 — Verify Peers
// ---------------------------------------------------------------------------

// TestProtocol_Phase1_SourceNotFound verifies that replication fails immediately
// with a descriptive error when the source database does not exist.
func TestProtocol_Phase1_SourceNotFound(t *testing.T) {
	source := NewMockPeer(false) // does not exist
	target := NewMockPeer(true)

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source database not available")
	assert.Equal(t, 0, result.DocsWritten)
}

// TestProtocol_Phase1_TargetCreatedOnDemand verifies that when CreateTarget is
// true and the target does not exist, it is created before replication begins.
func TestProtocol_Phase1_TargetCreatedOnDemand(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(false) // does not exist

	source.AddDoc("doc1", "1-aaa", false, nil)
	source.AddDoc("doc2", "1-bbb", false, nil)

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target", CreateTarget: true}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, target.CreateDBCalls)
	assert.Equal(t, 2, result.DocsWritten)
	assert.Equal(t, 2, target.DocCount())
}

// ---------------------------------------------------------------------------
// Phase 3 — Find Common Ancestry (checkpointing)
// ---------------------------------------------------------------------------

// TestProtocol_Phase3_NoCheckpoint_FullReplication verifies that when no
// checkpoint exists the full source history is replicated.
func TestProtocol_Phase3_NoCheckpoint_FullReplication(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 5; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 5, result.DocsWritten)

	// A checkpoint must have been saved with a non-empty source_last_seq
	cpDoc, err := source.GetLocalDoc(context.Background(), r.RepID)
	require.NoError(t, err)
	assert.NotEmpty(t, cpDoc.Data["source_last_seq"])
}

// TestProtocol_Phase3_ValidCheckpoint_IncrementalReplication verifies that a
// second run with the same RepID only replicates newly added documents.
func TestProtocol_Phase3_ValidCheckpoint_IncrementalReplication(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 5; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)

	// First run — replicate all 5 existing docs
	_, err := r.Run(context.Background())
	require.NoError(t, err)

	// Add 3 more docs to the source
	for i := 5; i < 8; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	// Second run — same replicator (same RepID) should only write the 3 new docs
	result2, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, result2.DocsWritten)
	assert.Equal(t, 8, target.DocCount())
}

// TestProtocol_Phase3_SessionMismatch_FullReplication verifies that when the
// session_id stored on source and target disagree the checkpoint is discarded
// and replication falls back to a full scan from seq 0.
func TestProtocol_Phase3_SessionMismatch_FullReplication(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 5; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)

	// First run — establishes a valid checkpoint
	_, err := r.Run(context.Background())
	require.NoError(t, err)

	// Corrupt the session_id on source and target so they no longer match
	ctx := context.Background()
	cpID := r.RepID

	sDoc, err := source.GetLocalDoc(ctx, cpID)
	require.NoError(t, err)
	sDoc.Data["session_id"] = "corrupted-session-A"
	require.NoError(t, source.PutLocalDoc(ctx, sDoc))

	tDoc, err := target.GetLocalDoc(ctx, cpID)
	require.NoError(t, err)
	tDoc.Data["session_id"] = "corrupted-session-B"
	require.NoError(t, target.PutLocalDoc(ctx, tDoc))

	// Second run — checkpoint mismatch forces since="" (full scan).
	// Target already holds all 5 docs with the correct revisions, so
	// RevsDiff returns nothing missing and DocsWritten is 0.
	result2, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result2.DocsWritten)
	assert.Equal(t, 5, target.DocCount())
}

// ---------------------------------------------------------------------------
// Phase 4 — Locate Changed Documents (RevsDiff)
// ---------------------------------------------------------------------------

// TestProtocol_Phase4_AllPresent_ZeroWrites verifies that when the target
// already has every revision from the source, nothing is written.
func TestProtocol_Phase4_AllPresent_ZeroWrites(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 3; i++ {
		rev := fmt.Sprintf("1-%032d", i)
		source.AddDoc(fmt.Sprintf("doc%d", i), rev, false, nil)
		target.AddDoc(fmt.Sprintf("doc%d", i), rev, false, nil) // same rev
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, result.DocsWritten)
}

// TestProtocol_Phase4_PartialMissing verifies that only the documents not
// already present on the target are fetched and written.
func TestProtocol_Phase4_PartialMissing(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 10; i++ {
		rev := fmt.Sprintf("1-%032d", i)
		source.AddDoc(fmt.Sprintf("doc%d", i), rev, false, nil)
	}
	// Target already has the first 4 docs with matching revisions
	for i := 0; i < 4; i++ {
		rev := fmt.Sprintf("1-%032d", i)
		target.AddDoc(fmt.Sprintf("doc%d", i), rev, false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 6, result.DocsWritten)
	assert.Equal(t, 10, target.DocCount())
}

// ---------------------------------------------------------------------------
// Phase 5 — Replicate Changes
// ---------------------------------------------------------------------------

// TestProtocol_Phase5_BatchBoundary_Exactly100Docs verifies that replication
// completes cleanly when the source has exactly changesBatchSize documents.
func TestProtocol_Phase5_BatchBoundary_Exactly100Docs(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < changesBatchSize; i++ {
		source.AddDoc(fmt.Sprintf("doc%04d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, changesBatchSize, result.DocsWritten)
	assert.Equal(t, changesBatchSize, target.DocCount())
}

// TestProtocol_Phase5_MultiBatch_250Docs verifies that all documents are
// replicated correctly when the source requires multiple batches (3 batches of
// 100 / 100 / 50).
func TestProtocol_Phase5_MultiBatch_250Docs(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	const total = 250
	for i := 0; i < total; i++ {
		source.AddDoc(fmt.Sprintf("doc%04d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, total, result.DocsWritten)
	assert.Equal(t, total, target.DocCount())
}

// TestProtocol_Phase5_TombstoneReplicatedCorrectly verifies that a deleted
// document (tombstone) is replicated to the target with Deleted=true and the
// original revision preserved.
func TestProtocol_Phase5_TombstoneReplicatedCorrectly(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	source.AddDoc("doc1", "2-abc", true, nil) // deleted tombstone

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.DocsWritten)

	doc, err := target.GetDoc(context.Background(), "doc1", false, nil)
	require.NoError(t, err)
	assert.True(t, doc.Deleted)
	assert.Equal(t, "2-abc", doc.Rev)
}

// TestProtocol_Phase5_RevisionPreserved verifies that replication preserves
// the exact source revision on the target (new_edits=false semantics).
func TestProtocol_Phase5_RevisionPreserved(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	rev := "7-deadbeef00000000000000000000000000000000"
	source.AddDoc("doc1", rev, false, map[string]interface{}{"content": "test"})

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.DocsWritten)

	doc, err := target.GetDoc(context.Background(), "doc1", false, nil)
	require.NoError(t, err)
	assert.Equal(t, rev, doc.Rev)
}

// TestProtocol_Phase5_CheckpointSavedToBothPeers verifies that after a
// successful replication run the checkpoint document is written to both the
// source and the target with identical source_last_seq and session_id values.
func TestProtocol_Phase5_CheckpointSavedToBothPeers(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 5; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	_, err := r.Run(context.Background())
	require.NoError(t, err)

	ctx := context.Background()
	cpID := r.RepID

	sDoc, err := source.GetLocalDoc(ctx, cpID)
	require.NoError(t, err)
	require.NotNil(t, sDoc)

	tDoc, err := target.GetLocalDoc(ctx, cpID)
	require.NoError(t, err)
	require.NotNil(t, tDoc)

	assert.NotEmpty(t, sDoc.Data["source_last_seq"])
	assert.NotEmpty(t, sDoc.Data["session_id"])

	// Both copies must agree on seq and session
	assert.Equal(t, sDoc.Data["source_last_seq"], tDoc.Data["source_last_seq"])
	assert.Equal(t, sDoc.Data["session_id"], tDoc.Data["session_id"])
}
