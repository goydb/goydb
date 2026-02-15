package controller

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupReplicationTest(t *testing.T) (*storage.Storage, func()) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-repl-ctrl-*")
	require.NoError(t, err)

	s, err := storage.Open(dir)
	require.NoError(t, err)

	return s, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestReplicationController_CompletesOneShot(t *testing.T) {
	s, cleanup := setupReplicationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create source DB with docs
	srcDB, err := s.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		_, err := srcDB.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	// Create target DB (empty)
	_, err = s.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	// Create _replicator DB
	repDB, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	// Create replication doc
	_, err = repDB.PutDocument(ctx, &model.Document{
		ID: "rep1",
		Data: map[string]interface{}{
			"source":     "sourcedb",
			"target":     "targetdb",
			"continuous": false,
		},
	})
	require.NoError(t, err)

	// Start controller
	rc := &Replication{Storage: s}
	ctrlCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	go rc.Run(ctrlCtx)

	// Wait for replication to complete
	require.Eventually(t, func() bool {
		doc, err := repDB.GetDocument(ctx, "rep1")
		if err != nil || doc == nil {
			return false
		}
		state, _ := doc.Data["_replication_state"].(string)
		return state == string(model.ReplicationStateCompleted)
	}, 15*time.Second, 500*time.Millisecond)

	// Verify target has docs
	tgtDB, err := s.Database(ctx, "targetdb")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		doc, err := tgtDB.GetDocument(ctx, fmt.Sprintf("doc%d", i))
		require.NoError(t, err)
		assert.NotNil(t, doc)
	}

	cancel()
}

func TestReplicationController_IgnoresDesignDocs(t *testing.T) {
	s, cleanup := setupReplicationTest(t)
	defer cleanup()

	ctx := context.Background()

	repDB, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	// Put a design doc
	_, err = repDB.PutDocument(ctx, &model.Document{
		ID:   "_design/test",
		Data: map[string]interface{}{"views": map[string]interface{}{}},
	})
	require.NoError(t, err)

	rc := &Replication{Storage: s}
	rc.active = make(map[string]context.CancelFunc)

	// Process once
	rc.processReplicatorDB(ctx)

	// Should not have started any replications
	rc.mu.Lock()
	count := len(rc.active)
	rc.mu.Unlock()
	assert.Equal(t, 0, count)
}

func TestReplicationController_ErrorState(t *testing.T) {
	s, cleanup := setupReplicationTest(t)
	defer cleanup()

	ctx := context.Background()

	repDB, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	// Create replication doc with nonexistent source
	_, err = repDB.PutDocument(ctx, &model.Document{
		ID: "bad-rep",
		Data: map[string]interface{}{
			"source":     "nonexistent",
			"target":     "alsonothere",
			"continuous": false,
		},
	})
	require.NoError(t, err)

	rc := &Replication{Storage: s}
	ctrlCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	go rc.Run(ctrlCtx)

	// Wait for error state
	require.Eventually(t, func() bool {
		doc, err := repDB.GetDocument(ctx, "bad-rep")
		if err != nil || doc == nil {
			return false
		}
		state, _ := doc.Data["_replication_state"].(string)
		return state == string(model.ReplicationStateError)
	}, 15*time.Second, 500*time.Millisecond)

	cancel()
}

func TestReplicationController_CancelAllOnShutdown(t *testing.T) {
	s, cleanup := setupReplicationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create source and target DBs
	srcDB, err := s.CreateDatabase(ctx, "src1")
	require.NoError(t, err)
	_, err = srcDB.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"v": 1},
	})
	require.NoError(t, err)
	_, err = s.CreateDatabase(ctx, "tgt1")
	require.NoError(t, err)

	srcDB2, err := s.CreateDatabase(ctx, "src2")
	require.NoError(t, err)
	_, err = srcDB2.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"v": 1},
	})
	require.NoError(t, err)
	_, err = s.CreateDatabase(ctx, "tgt2")
	require.NoError(t, err)

	repDB, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	// Start 2 continuous replications
	_, err = repDB.PutDocument(ctx, &model.Document{
		ID: "rep1",
		Data: map[string]interface{}{
			"source":     "src1",
			"target":     "tgt1",
			"continuous": true,
		},
	})
	require.NoError(t, err)

	_, err = repDB.PutDocument(ctx, &model.Document{
		ID: "rep2",
		Data: map[string]interface{}{
			"source":     "src2",
			"target":     "tgt2",
			"continuous": true,
		},
	})
	require.NoError(t, err)

	rc := &Replication{Storage: s}
	ctrlCtx, cancel := context.WithCancel(ctx)

	done := make(chan struct{})
	go func() {
		rc.Run(ctrlCtx)
		close(done)
	}()

	// Wait for replications to start
	time.Sleep(7 * time.Second)

	// Cancel and verify clean shutdown
	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("controller did not stop after cancellation")
	}

	rc.mu.Lock()
	assert.Empty(t, rc.active)
	rc.mu.Unlock()
}
