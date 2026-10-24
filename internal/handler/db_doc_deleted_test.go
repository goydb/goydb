package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
		"_id":   "doc1",
		"_rev":  tombstoneRev,
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
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	code, result := deleteDoc(t, router, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)
	tombstoneRev := result["rev"].(string)

	// Recreate the document without supplying any revision.
	code, result = putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"hello": "recreated",
	})
	require.Equal(t, http.StatusCreated, code)
	newRev := result["rev"].(string)
	assert.Equal(t, docRevGen(t, tombstoneRev)+1, docRevGen(t, newRev))

	code, body := getDoc(t, router, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "recreated", body["hello"])
	assert.Equal(t, newRev, body["_rev"])
}

// A PUT carrying a revision that is no longer a leaf still conflicts, even when
// the stored document is a tombstone — CouchDB only waives the check when no
// revision is supplied.
func TestDeletedDoc_RecreateWithStaleRevConflicts(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	code, _ := deleteDoc(t, router, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"_rev":  rev, // pre-delete revision, no longer a leaf
		"hello": "recreated",
	})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "conflict", result["error"])
}

// Guard against over-reach: a live document still requires a matching _rev.
func TestDoc_PutWithoutRevOnLiveDocConflicts(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	createDoc(t, router, "testdb", "doc1")

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"hello": "overwrite",
	})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "conflict", result["error"])
}

// _bulk_docs takes the same path, so a rev-less entry over a tombstone is
// accepted too.
func TestDeletedDoc_BulkDocsRecreateWithoutRev(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, router, "testdb", "doc1")

	code, _ := deleteDoc(t, router, "/testdb/doc1?rev="+rev)
	require.Equal(t, http.StatusOK, code)

	code, body := bulkDocs(t, router, "testdb", `{"docs":[{"_id":"doc1","hello":"recreated"}]}`)
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body, "conflict")

	code, doc := getDoc(t, router, "/testdb/doc1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "recreated", doc["hello"])
}
