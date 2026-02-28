package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func headDoc(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("HEAD", path, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestHeadDoc_ReturnsETag(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	w := headDoc(t, router, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `"`+rev+`"`, w.Header().Get("ETag"))
}

func TestHeadDoc_XCouchFullCommitHeader(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, router, "testdb", "doc1")

	w := headDoc(t, router, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "true", w.Header().Get("X-Couch-Full-Commit"))
}

func TestHeadDoc_RevParam(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	// Request with correct rev should return 200
	w := headDoc(t, router, "/testdb/doc1?rev="+rev)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `"`+rev+`"`, w.Header().Get("ETag"))
}

func TestHeadDoc_RevParam_NotFound(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, router, "testdb", "doc1")

	// Request with non-existent rev should return 404
	w := headDoc(t, router, "/testdb/doc1?rev=999-bogus")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHeadDoc_NotFound(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	w := headDoc(t, router, "/testdb/nonexistent")
	assert.Equal(t, http.StatusNotFound, w.Code)
}
