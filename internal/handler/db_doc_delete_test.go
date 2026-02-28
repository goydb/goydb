package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createDoc(t *testing.T, router http.Handler, db, id string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]interface{}{"_id": id, "hello": "world"})
	req := httptest.NewRequest("PUT", "/"+db+"/"+id, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	return resp["rev"].(string)
}

func TestDeleteDoc_BatchOk(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	code, result := deleteDoc(t, router, "/testdb/doc1?rev="+rev+"&batch=ok")

	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

func TestDeleteDoc_WithoutBatch(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	code, result := deleteDoc(t, router, "/testdb/doc1?rev="+rev)

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}
