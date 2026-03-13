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

func TestDeletedDoc_GET_ReturnsETag(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	// Delete the document
	code, result := deleteDoc(t, router, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// GET should return 404 with ETag containing the tombstone rev
	code, _ = getDoc(t, router, "/testdb/doc1")
	assert.Equal(t, http.StatusNotFound, code)

	// Use headDoc-style request to check headers (getDoc doesn't return headers)
	req := httptest.NewRequest("GET", "/testdb/doc1", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, `"`+tombstoneRev+`"`, w.Header().Get("ETag"))
}

func TestDeletedDoc_HEAD_ReturnsETag(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	// Delete the document
	code, result := deleteDoc(t, router, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// HEAD should return 404 with ETag containing the tombstone rev
	w := headDoc(t, router, "/testdb/doc1")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, `"`+tombstoneRev+`"`, w.Header().Get("ETag"))
}

func TestDeletedDoc_RecreateWithTombstoneRev(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	// Delete the document
	code, result := deleteDoc(t, router, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// Recreate document using the tombstone rev
	b, _ := json.Marshal(map[string]interface{}{
		"_id":  "doc1",
		"_rev": tombstoneRev,
		"hello": "recreated",
	})
	req := httptest.NewRequest("PUT", "/testdb/doc1", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify the document is accessible again
	code, body := getDoc(t, router, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "recreated", body["hello"])
}

func TestDeletedDoc_GET_ReturnsETag_OnSuccessfulGet(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	// GET should return 200 with ETag
	req := httptest.NewRequest("GET", "/testdb/doc1", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `"`+rev+`"`, w.Header().Get("ETag"))
}
