package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRevsDiffTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-handler-test-*")
	require.NoError(t, err)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:            s,
		SessionStore:       store,
		Admins:             model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication: &service.Replication{Storage: s, Logger: logger.NewNoLog()}, Logger: logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	return s, r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestRevsDiff_AllMissing(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string][]string{
		"doc1": {"1-a"},
		"doc2": {"1-b"},
	})

	req := httptest.NewRequest("POST", "/testdb/_revs_diff", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string][]string
	json.NewDecoder(w.Body).Decode(&result) // nolint: errcheck
	assert.Contains(t, result, "doc1")
	assert.Contains(t, result, "doc2")
}

func TestRevsDiff_SomePresent(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string][]string{
		"doc1": {rev},
		"doc2": {"1-b"},
	})

	req := httptest.NewRequest("POST", "/testdb/_revs_diff", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string][]string
	json.NewDecoder(w.Body).Decode(&result) // nolint: errcheck
	assert.NotContains(t, result, "doc1")
	assert.Contains(t, result, "doc2")
}

func TestRevsDiff_NoneMissing(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string][]string{
		"doc1": {rev},
	})

	req := httptest.NewRequest("POST", "/testdb/_revs_diff", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string][]string
	json.NewDecoder(w.Body).Decode(&result) // nolint: errcheck
	assert.Empty(t, result)
}

func TestRevsDiff_InvalidJSON(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/testdb/_revs_diff", bytes.NewReader([]byte("not json")))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRevsDiff_NonexistentDB(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body, _ := json.Marshal(map[string][]string{"doc1": {"1-a"}})

	req := httptest.NewRequest("POST", "/nonexistent/_revs_diff", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRevsDiff_AncestorRevNotMissing(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
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

	// POST rev1 (now an ancestor of the current rev).
	body, _ := json.Marshal(map[string][]string{
		"doc1": {rev1},
	})

	req := httptest.NewRequest("POST", "/testdb/_revs_diff", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string][]string
	json.NewDecoder(w.Body).Decode(&result) //nolint: errcheck
	assert.NotContains(t, result, "doc1", "ancestor rev should not appear in _revs_diff result")
}

func TestRevsDiff_ConflictLeafNotMissing(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Replicate two same-gen conflict leaves.
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

	// Both conflict revs should not be reported as missing.
	body, _ := json.Marshal(map[string][]string{
		"doc1": {"1-aaa", "1-zzz"},
	})
	req := httptest.NewRequest("POST", "/testdb/_revs_diff", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var result map[string]map[string][]string
	json.NewDecoder(w.Body).Decode(&result) //nolint:errcheck
	assert.NotContains(t, result, "doc1", "conflict leaf revs should not appear as missing")
}

func TestAllDocs_TotalRowsExcludesDeleted(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev1, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc3",
		Data: map[string]interface{}{"x": 3},
	})
	require.NoError(t, err)

	// Delete doc1 — it becomes a tombstone in the docs bucket
	_, err = db.DeleteDocument(ctx, "doc1", rev1)
	require.NoError(t, err)

	// total_rows must reflect only live documents (2), not the tombstone
	req := httptest.NewRequest("GET", "/testdb/_all_docs", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result AllDocsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, 2, result.TotalRows)
	assert.Len(t, result.Rows, 2)
}
