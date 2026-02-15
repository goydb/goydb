package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicate_LocalToLocal(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
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

	body, _ := json.Marshal(map[string]interface{}{
		"source": "sourcedb",
		"target": "targetdb",
	})
	req := httptest.NewRequest("POST", "/_replicate", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])

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
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	src, err := s.CreateDatabase(ctx, "sourcedb")
	require.NoError(t, err)
	_, err = src.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 42},
	})
	require.NoError(t, err)

	// Target does NOT exist — create_target should create it
	body, _ := json.Marshal(map[string]interface{}{
		"source":        "sourcedb",
		"target":        "newdb",
		"create_target": true,
	})
	req := httptest.NewRequest("POST", "/_replicate", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])

	tgt, err := s.Database(ctx, "newdb")
	require.NoError(t, err)
	doc, err := tgt.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestReplicate_MissingSourceOrTarget(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]interface{}{"source": "only-source"})
	req := httptest.NewRequest("POST", "/_replicate", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestReplicate_AllDocsCountAfterReplication mirrors the Fauxton verify-install
// replication check: source has 4 live docs (one deleted tombstone), and after
// replication the target's _all_docs total_rows must be exactly 4 — not 5
// (tombstone) and not 5 (checkpoint _local doc).
func TestReplicate_AllDocsCountAfterReplication(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
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

	body, _ := json.Marshal(map[string]interface{}{
		"source": "sourcedb",
		"target": "targetdb",
	})
	req := httptest.NewRequest("POST", "/_replicate", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// _all_docs on the target must report exactly 4 (excludes tombstone and
	// the _local checkpoint doc written by the replicator).
	req = httptest.NewRequest("GET", "/targetdb/_all_docs", nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var result AllDocsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, 4, result.TotalRows)
	assert.Len(t, result.Rows, 4)
}
