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

func TestIndexPost_Create(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{
		"index": map[string]interface{}{
			"fields": []string{"type"},
		},
		"name": "by_type",
		"ddoc": "mango",
		"type": "json",
	})

	req := httptest.NewRequest("POST", "/testdb/_index", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "created", resp["result"])
	assert.Equal(t, "_design/mango", resp["id"])
	assert.Equal(t, "by_type", resp["name"])

	// Design doc should exist in DB
	db, err := s.Database(ctx, "testdb")
	require.NoError(t, err)
	doc, err := db.GetDocument(ctx, "_design/mango")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestIndexPost_Exists(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{
		"index": map[string]interface{}{"fields": []string{"status"}},
		"name":  "by_status",
		"ddoc":  "mango",
		"type":  "json",
	})

	// First POST → 201
	req := httptest.NewRequest("POST", "/testdb/_index", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Second identical POST → 200 exists
	req2 := httptest.NewRequest("POST", "/testdb/_index", bytes.NewReader(body))
	req2.SetBasicAuth("admin", "secret")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&resp))
	assert.Equal(t, "exists", resp["result"])
}

func TestIndexGet_Empty(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/testdb/_index", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	// Only the built-in _all_docs index
	assert.Equal(t, float64(1), resp["total_rows"])
}

func TestIndexGet_WithIndex(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{
		"index": map[string]interface{}{"fields": []string{"type"}},
		"name":  "by_type",
		"ddoc":  "mango",
		"type":  "json",
	})
	req := httptest.NewRequest("POST", "/testdb/_index", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	router.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("GET", "/testdb/_index", nil)
	req2.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req2)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(2), resp["total_rows"])
}

func TestIndexDelete_OK(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create index
	body, _ := json.Marshal(map[string]interface{}{
		"index": map[string]interface{}{"fields": []string{"type"}},
		"name":  "by_type",
		"ddoc":  "mango",
		"type":  "json",
	})
	req := httptest.NewRequest("POST", "/testdb/_index", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	router.ServeHTTP(httptest.NewRecorder(), req)

	// Delete index
	delReq := httptest.NewRequest("DELETE", "/testdb/_index/mango/json/by_type", nil)
	delReq.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, delReq)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp["ok"])

	// GET _index should only have built-in now
	getReq := httptest.NewRequest("GET", "/testdb/_index", nil)
	getReq.SetBasicAuth("admin", "secret")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, getReq)
	var listResp map[string]interface{}
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&listResp))
	assert.Equal(t, float64(1), listResp["total_rows"])
}

func TestIndexDelete_NotFound(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	delReq := httptest.NewRequest("DELETE", "/testdb/_index/nonexistent/json/myindex", nil)
	delReq.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, delReq)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestFind_UsesMangoIndex(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create Mango index on "type" field
	body, _ := json.Marshal(map[string]interface{}{
		"index": map[string]interface{}{"fields": []string{"type"}},
		"name":  "by_type",
		"ddoc":  "mango",
		"type":  "json",
	})
	req := httptest.NewRequest("POST", "/testdb/_index", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	router.ServeHTTP(httptest.NewRecorder(), req)

	// Insert some documents
	for i := 0; i < 5; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("animal_%d", i),
			Data: map[string]interface{}{"type": "animal", "name": fmt.Sprintf("creature_%d", i)},
		})
		require.NoError(t, err)
	}
	for i := 0; i < 3; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("plant_%d", i),
			Data: map[string]interface{}{"type": "plant", "name": fmt.Sprintf("flora_%d", i)},
		})
		require.NoError(t, err)
	}

	// Find animals
	findBody, _ := json.Marshal(map[string]interface{}{
		"selector": map[string]interface{}{
			"type": map[string]interface{}{"$eq": "animal"},
		},
		"execution_stats": true,
	})
	findReq := httptest.NewRequest("POST", "/testdb/_find", bytes.NewReader(findBody))
	findReq.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, findReq)

	assert.Equal(t, http.StatusOK, w.Code)
	var findResp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&findResp))
	docs, _ := findResp["docs"].([]interface{})
	assert.Len(t, docs, 5)
}
