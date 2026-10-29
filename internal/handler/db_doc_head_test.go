package handler

import (
	"net/http"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func headDoc(t *testing.T, c *client.Client, path string) *http.Response {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodHead, path, nil, nil)
	require.NoError(t, err)
	return resp
}

func TestHeadDoc_ReturnsETag(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	resp := headDoc(t, c, "/testdb/doc1")
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `"`+rev+`"`, resp.Header.Get("ETag"))
}

func TestHeadDoc_XCouchFullCommitHeader(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, c, "testdb", "doc1")

	resp := headDoc(t, c, "/testdb/doc1")
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "true", resp.Header.Get("X-Couch-Full-Commit"))
}

func TestHeadDoc_RevParam(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	// Request with correct rev should return 200
	resp := headDoc(t, c, "/testdb/doc1?rev="+rev)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `"`+rev+`"`, resp.Header.Get("ETag"))
}

func TestHeadDoc_RevParam_NotFound(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, c, "testdb", "doc1")

	// Request with non-existent rev should return 404
	resp := headDoc(t, c, "/testdb/doc1?rev=999-bogus")
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHeadDoc_NotFound(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	resp := headDoc(t, c, "/testdb/nonexistent")
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
