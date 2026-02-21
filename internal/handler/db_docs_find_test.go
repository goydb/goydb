package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFind_FieldsProjection(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "Alice", "age": 30},
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{
		"selector": map[string]interface{}{"name": "Alice"},
		"fields":   []string{"name"},
	})
	req := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp FindResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Docs, 1)

	doc := resp.Docs[0]
	assert.Equal(t, "doc1", doc["_id"])
	assert.NotEmpty(t, doc["_rev"])
	assert.Equal(t, "Alice", doc["name"])
	assert.Nil(t, doc["age"], "age should not be projected")
}

func TestFind_FieldsProjection_Nested(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID: "doc1",
		Data: map[string]interface{}{
			"meta": map[string]interface{}{
				"score": 99,
				"tag":   "go",
			},
			"title": "Test",
		},
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{
		"selector": map[string]interface{}{"title": "Test"},
		"fields":   []string{"meta.score"},
	})
	req := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp FindResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Docs, 1)

	doc := resp.Docs[0]
	meta, ok := doc["meta"].(map[string]interface{})
	require.True(t, ok, "meta should be a map")
	assert.Equal(t, float64(99), meta["score"])
	assert.Nil(t, meta["tag"], "tag should not be projected")
	assert.Nil(t, doc["title"], "title should not be projected")
}

func TestFind_SortAsc(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, n := range []int{3, 1, 2} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", n),
			Data: map[string]interface{}{"n": n},
		})
		require.NoError(t, err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"selector": map[string]interface{}{"n": map[string]interface{}{"$gt": 0}},
		"sort":     []interface{}{map[string]string{"n": "asc"}},
	})
	req := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp FindResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Docs, 3)

	assert.Equal(t, float64(1), resp.Docs[0]["n"])
	assert.Equal(t, float64(2), resp.Docs[1]["n"])
	assert.Equal(t, float64(3), resp.Docs[2]["n"])
}

func TestFind_SortDesc(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, n := range []int{3, 1, 2} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", n),
			Data: map[string]interface{}{"n": n},
		})
		require.NoError(t, err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"selector": map[string]interface{}{"n": map[string]interface{}{"$gt": 0}},
		"sort":     []interface{}{map[string]string{"n": "desc"}},
	})
	req := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp FindResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Docs, 3)

	assert.Equal(t, float64(3), resp.Docs[0]["n"])
	assert.Equal(t, float64(2), resp.Docs[1]["n"])
	assert.Equal(t, float64(1), resp.Docs[2]["n"])
}

func TestFind_SortWithLimit(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Insert in storage order: 5, 1, 3, 2, 4
	for _, n := range []int{5, 1, 3, 2, 4} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", n),
			Data: map[string]interface{}{"n": n},
		})
		require.NoError(t, err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"selector": map[string]interface{}{"n": map[string]interface{}{"$gt": 0}},
		"sort":     []interface{}{map[string]string{"n": "asc"}},
		"limit":    2,
	})
	req := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp FindResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Docs, 2)

	// Should return the 2 smallest values (1, 2), not the first 2 scanned (5, 1 or similar).
	assert.Equal(t, float64(1), resp.Docs[0]["n"])
	assert.Equal(t, float64(2), resp.Docs[1]["n"])
}

func TestFind_UseIndex(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a Mango index on "type" field under ddoc "mango", name "by_type".
	indexBody, _ := json.Marshal(map[string]interface{}{
		"index": map[string]interface{}{"fields": []string{"type"}},
		"name":  "by_type",
		"ddoc":  "mango",
		"type":  "json",
	})
	idxReq := httptest.NewRequest("POST", "/testdb/_index", bytes.NewReader(indexBody))
	idxReq.SetBasicAuth("admin", "secret")
	router.ServeHTTP(httptest.NewRecorder(), idxReq)

	for i := 0; i < 3; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"type": "widget"},
		})
		require.NoError(t, err)
	}

	// Query with use_index pointing at our mango index.
	findBody, _ := json.Marshal(map[string]interface{}{
		"selector":        map[string]interface{}{"type": "widget"},
		"use_index":       []interface{}{"mango", "by_type"},
		"execution_stats": true,
	})
	req := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(findBody))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	docs, _ := resp["docs"].([]interface{})
	assert.Len(t, docs, 3)

	// With execution_stats, total_keys_examined > 0 when the index is used.
	stats, _ := resp["execution_stats"].(map[string]interface{})
	require.NotNil(t, stats)
	assert.Equal(t, float64(3), stats["total_keys_examined"])
}

func TestFind_NoopParams(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{
		"selector":  map[string]interface{}{"x": 1},
		"r":         1,
		"q":         1,
		"stable":    true,
		"conflicts": false,
		"update":    "true",
	})
	req := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp FindResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Len(t, resp.Docs, 1)
}
