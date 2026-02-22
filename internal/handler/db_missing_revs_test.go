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

func TestMissingRevs_AllMissing(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string][]string{
		"doc1": {"1-a"},
		"doc2": {"1-b"},
	})

	req := httptest.NewRequest("POST", "/testdb/_missing_revs", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string][]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	missingRevs := result["missing_revs"]
	assert.Contains(t, missingRevs, "doc1")
	assert.Contains(t, missingRevs, "doc2")
}

func TestMissingRevs_CurrentRevPresent(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string][]string{
		"doc1": {rev},
	})

	req := httptest.NewRequest("POST", "/testdb/_missing_revs", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string][]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	missingRevs := result["missing_revs"]
	assert.NotContains(t, missingRevs, "doc1")
}

func TestMissingRevs_AncestorRevNotMissing(t *testing.T) {
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

	// rev1 is now an ancestor — must not appear in missing_revs.
	body, _ := json.Marshal(map[string][]string{
		"doc1": {rev1},
	})

	req := httptest.NewRequest("POST", "/testdb/_missing_revs", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string][]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	missingRevs := result["missing_revs"]
	assert.NotContains(t, missingRevs, "doc1", "ancestor rev should not be reported as missing")
}

func TestMissingRevs_ConflictLeafNotMissing(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
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

	body, _ := json.Marshal(map[string][]string{
		"doc1": {"1-aaa", "1-zzz"},
	})
	req := httptest.NewRequest("POST", "/testdb/_missing_revs", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var result map[string]map[string][]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	missingRevs := result["missing_revs"]
	assert.NotContains(t, missingRevs, "doc1", "conflict leaf revs should not appear as missing")
}

func TestMissingRevs_InvalidJSON(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/testdb/_missing_revs", bytes.NewReader([]byte("not json")))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
