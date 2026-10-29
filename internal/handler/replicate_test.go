package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicate_LocalToLocal(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	// Create source DB with a couple of docs
	src, err := s.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = src.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)
	_, err = src.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Create empty target DB
	_, err = s.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	_, err = c.Replicate(ctx, &model.ReplicationDoc{
		Source: "sourcedb",
		Target: "targetdb",
	})
	require.NoError(t, err)

	// Verify docs appear in target
	tgt, err := s.Database(ctx, "targetdb")
	require.NoError(t, err)
	doc, err := tgt.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	doc, err = tgt.GetDocument(ctx, "doc2")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestReplicate_CreateTarget(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	src, err := s.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = src.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 42},
	})
	require.NoError(t, err)

	// Target does NOT exist — create_target should create it. The
	// "create_target" field doesn't round-trip through model.ReplicationDoc
	// (it has no JSON tags, and the field's camelCase doesn't
	// case-insensitively match the server's snake_case tag), so send it
	// directly via Raw.
	body, _ := json.Marshal(map[string]interface{}{
		"source":        "sourcedb",
		"target":        "newdb",
		"create_target": true,
	})
	resp, err := c.Raw(ctx, http.MethodPost, "/_replicate", bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])

	tgt, err := s.Database(ctx, "newdb")
	require.NoError(t, err)
	doc, err := tgt.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestReplicate_MissingSourceOrTarget(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := c.Replicate(t.Context(), &model.ReplicationDoc{Source: "only-source"})
	assert.Equal(t, http.StatusBadRequest, statusCode(t, err, http.StatusOK))
}

// TestReplicate_AllDocsCountAfterReplication mirrors the Fauxton verify-install
// replication check: source has 4 live docs (one deleted tombstone), and after
// replication the target's _all_docs total_rows must be exactly 4 — not 5
// (tombstone) and not 5 (checkpoint _local doc).
func TestReplicate_AllDocsCountAfterReplication(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	src, err := s.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)

	// doc_a will be deleted — becomes a tombstone
	rev, err := src.PutDocument(ctx, &model.Document{
		ID:   "doc_a",
		Data: map[string]interface{}{"x": 0},
	})
	require.NoError(t, err)
	_, err = src.DeleteDocument(ctx, "doc_a", rev)
	require.NoError(t, err)

	// 3 live regular docs + 1 design doc = 4 expected
	for _, id := range []string{"doc_b", "doc_c", "doc_d"} {
		_, err = src.PutDocument(ctx, &model.Document{
			ID:   id,
			Data: map[string]interface{}{"v": id},
		})
		require.NoError(t, err)
	}
	_, err = src.PutDocument(ctx, &model.Document{
		ID:   "_design/myview",
		Data: map[string]interface{}{"views": map[string]interface{}{}},
	})
	require.NoError(t, err)

	_, err = s.CreateDatabase(ctx, "targetdb")
	require.NoError(t, err)

	_, err = c.Replicate(ctx, &model.ReplicationDoc{
		Source: "sourcedb",
		Target: "targetdb",
	})
	require.NoError(t, err)

	// _all_docs on the target must report exactly 4 (excludes tombstone and
	// the _local checkpoint doc written by the replicator).
	result, err := c.DB("targetdb").AllDocs(ctx, client.AllDocsQuery{})
	require.NoError(t, err)
	assert.Equal(t, 4, result.TotalRows)
	assert.Len(t, result.Rows, 4)
}
