package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBDocGet_RevisionsOnlyWithRevsParam(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-revisions-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s.Close()

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.Router{}
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(&r)
	require.NoError(t, err)

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "test", "value": 123},
	})
	require.NoError(t, err)

	// Test 1: GET without revs parameter should NOT include _revisions
	req := httptest.NewRequest("GET", "/testdb/doc1", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	assert.NotNil(t, result["_id"])
	assert.NotNil(t, result["_rev"])
	assert.Nil(t, result["_revisions"], "_revisions should NOT be present without revs=true")

	// Test 2: GET with revs=true should include _revisions
	req = httptest.NewRequest("GET", "/testdb/doc1?revs=true", nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	assert.NotNil(t, result["_id"])
	assert.NotNil(t, result["_rev"])
	assert.NotNil(t, result["_revisions"], "_revisions should be present with revs=true")

	// Test 3: Subsequent GET without revs should NOT include _revisions
	req = httptest.NewRequest("GET", "/testdb/doc1", nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	result = nil // reset to avoid stale keys from previous decode
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	assert.NotNil(t, result["_id"])
	assert.NotNil(t, result["_rev"])
	assert.Nil(t, result["_revisions"], "_revisions should NOT persist after revs=true request")
}

func TestDBDocGet_RevisionsChainGrowsWithUpdates(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-revchain-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s.Close()

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

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

	req := httptest.NewRequest("GET", "/testdb/doc1?revs=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	revisions, ok := result["_revisions"].(map[string]interface{})
	require.True(t, ok, "_revisions should be present with revs=true")

	start, ok := revisions["start"].(float64)
	require.True(t, ok, "start should be a number")
	assert.Equal(t, float64(2), start, "start should be 2 after two updates")

	ids, ok := revisions["ids"].([]interface{})
	require.True(t, ok, "ids should be an array")
	assert.Len(t, ids, 2, "should have 2 revision IDs in the chain")
}

func setupRevisionTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-revtest-*")
	require.NoError(t, err)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))
	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	return s, r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestDocGet_ConflictsField(t *testing.T) {
	s, router, cleanup := setupRevisionTest(t)
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

	req := httptest.NewRequest("GET", "/testdb/doc1", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.NotNil(t, result["_conflicts"], "_conflicts should be populated when conflicts exist")
}

func TestDocGet_OpenRevsAll(t *testing.T) {
	s, router, cleanup := setupRevisionTest(t)
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

	req := httptest.NewRequest("GET", "/testdb/doc1?open_revs=all", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Len(t, result, 2)
}

func TestDocGet_OpenRevsSpecific(t *testing.T) {
	s, router, cleanup := setupRevisionTest(t)
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

	req := httptest.NewRequest(`GET`, `/testdb/doc1?open_revs=%5B%221-aaa%22%5D`, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result, 1)
	assert.NotNil(t, result[0]["ok"])
}

func TestDocGet_OpenRevsMissingRev(t *testing.T) {
	s, router, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(`GET`, `/testdb/doc1?open_revs=%5B%223-nonexistent%22%5D`, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result, 1)
	assert.Equal(t, "3-nonexistent", result[0]["missing"])
}

func TestAllDocs_NoRevisions(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-allrevisions-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s.Close()

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "test"},
	})
	require.NoError(t, err)

	// Query _all_docs with include_docs=true
	req := httptest.NewRequest("GET", "/testdb/_all_docs?include_docs=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result AllDocsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	require.Len(t, result.Rows, 1)
	doc := result.Rows[0].Doc

	assert.NotNil(t, doc["_id"])
	assert.NotNil(t, doc["_rev"])
	assert.Equal(t, "test", doc["name"])
	assert.Nil(t, doc["_revisions"], "_revisions should NOT be in _all_docs response")
}
