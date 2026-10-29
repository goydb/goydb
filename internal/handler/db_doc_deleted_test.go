package handler

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeletedDoc_GET_ReturnsETag(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	// Delete the document
	code, result := deleteDoc(t, c, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// GET should return 404 with ETag containing the tombstone rev
	code, _ = getDoc(t, c, "/testdb/doc1")
	assert.Equal(t, http.StatusNotFound, code)

	// getDoc doesn't return headers, so issue a raw GET to check the ETag.
	getResp, err := c.Raw(t.Context(), http.MethodGet, "/testdb/doc1", nil, nil)
	require.NoError(t, err)
	defer getResp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode)
	assert.Equal(t, `"`+tombstoneRev+`"`, getResp.Header.Get("ETag"))
}

func TestDeletedDoc_HEAD_ReturnsETag(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	// Delete the document
	code, result := deleteDoc(t, c, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// HEAD should return 404 with ETag containing the tombstone rev
	resp := headDoc(t, c, "/testdb/doc1")
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, `"`+tombstoneRev+`"`, resp.Header.Get("ETag"))
}

func TestDeletedDoc_RecreateWithTombstoneRev(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	// Delete the document
	code, result := deleteDoc(t, c, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// Recreate document using the tombstone rev
	code, _ = putDoc(t, c, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"_rev":  tombstoneRev,
		"hello": "recreated",
	})
	assert.Equal(t, http.StatusCreated, code)

	// Verify the document is accessible again
	code, body := getDoc(t, c, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "recreated", body["hello"])
}

func TestDeletedDoc_GET_ReturnsETag_OnSuccessfulGet(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	// GET should return 200 with ETag
	resp, err := c.Raw(t.Context(), http.MethodGet, "/testdb/doc1", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `"`+rev+`"`, resp.Header.Get("ETag"))
}

// docRevGen returns the generation number of a revision string.
func docRevGen(t *testing.T, rev string) int {
	t.Helper()
	gen, _, ok := strings.Cut(rev, "-")
	require.True(t, ok, "malformed rev %q", rev)
	n, err := strconv.Atoi(gen)
	require.NoError(t, err)
	return n
}

// CouchDB accepts a PUT without _rev to a deleted document and creates a new
// revision on top of the tombstone (issue #16).
func TestDeletedDoc_RecreateWithoutRev(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	code, result := deleteDoc(t, c, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// Recreate the document without supplying any revision.
	code, result = putDoc(t, c, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"hello": "recreated",
	})
	require.Equal(t, http.StatusCreated, code)
	newRev := result["rev"].(string)
	assert.Equal(t, docRevGen(t, tombstoneRev)+1, docRevGen(t, newRev))

	code, body := getDoc(t, c, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "recreated", body["hello"])
	assert.Equal(t, newRev, body["_rev"])
}

// A PUT carrying a revision that is no longer a leaf still conflicts, even when
// the stored document is a tombstone — CouchDB only waives the check when no
// revision is supplied.
func TestDeletedDoc_RecreateWithStaleRevConflicts(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	code, _ := deleteDoc(t, c, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)

	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"_rev":  rev, // pre-delete revision, no longer a leaf
		"hello": "recreated",
	})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "conflict", result["error"])
}

// Guard against over-reach: a live document still requires a matching _rev.
func TestDoc_PutWithoutRevOnLiveDocConflicts(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	createDoc(t, c, "testdb", "doc1")

	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"hello": "overwrite",
	})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "conflict", result["error"])
}

// _bulk_docs takes the same path, so a rev-less entry over a tombstone is
// accepted too.
func TestDeletedDoc_BulkDocsRecreateWithoutRev(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	code, _ := deleteDoc(t, c, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)

	code, body := bulkDocs(t, c, "testdb", `{"docs":[{"_id":"doc1","hello":"recreated"}]}`)
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body, "conflict")

	code, doc := getDoc(t, c, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "recreated", doc["hello"])
}
