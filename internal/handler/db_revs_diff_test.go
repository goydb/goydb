package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRevsDiffTest is the original shared test setup, now backed by
// setupClientTest (a real in-process HTTP server + pkg/client), with the
// storage/client values in the order most existing callers already expect.
func setupRevsDiffTest(t *testing.T) (*storage.Storage, *client.Client, func()) {
	t.Helper()
	c, s, cleanup := setupClientTest(t)
	return s, c, cleanup
}

// statusCode derives the HTTP status code a pkg/client call's outcome
// implies: okStatus on success, or the *client.StatusError's code on
// failure. Used where a test needs the exact status code rather than just
// success/failure.
func statusCode(t *testing.T, err error, okStatus int) int {
	t.Helper()
	if err == nil {
		return okStatus
	}
	var se *client.StatusError
	if errors.As(err, &se) {
		return se.StatusCode
	}
	t.Fatalf("unexpected transport error: %v", err)
	return 0
}

func TestRevsDiff_AllMissing(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	result, err := c.DB("testdb").RevsDiff(t.Context(), map[string][]string{
		"doc1": {"1-a"},
		"doc2": {"1-b"},
	})
	require.NoError(t, err)
	assert.Contains(t, result, "doc1")
	assert.Contains(t, result, "doc2")
}

func TestRevsDiff_SomePresent(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	result, err := c.DB("testdb").RevsDiff(t.Context(), map[string][]string{
		"doc1": {rev},
		"doc2": {"1-b"},
	})
	require.NoError(t, err)
	assert.NotContains(t, result, "doc1")
	assert.Contains(t, result, "doc2")
}

func TestRevsDiff_NoneMissing(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	result, err := c.DB("testdb").RevsDiff(t.Context(), map[string][]string{
		"doc1": {rev},
	})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRevsDiff_InvalidJSON(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	resp, err := c.DB("testdb").Raw(t.Context(), http.MethodPost, "/_revs_diff", strings.NewReader("not json"), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRevsDiff_NonexistentDB(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := c.DB("nonexistent").RevsDiff(t.Context(), map[string][]string{"doc1": {"1-a"}})
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, statusCode(t, err, http.StatusOK))
}

func TestRevsDiff_AncestorRevNotMissing(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
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

	// POST rev1 (now an ancestor of the current rev).
	result, err := c.DB("testdb").RevsDiff(t.Context(), map[string][]string{
		"doc1": {rev1},
	})
	require.NoError(t, err)
	assert.NotContains(t, result, "doc1", "ancestor rev should not appear in _revs_diff result")
}

func TestRevsDiff_ConflictLeafNotMissing(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Replicate two same-gen conflict leaves.
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

	// Both conflict revs should not be reported as missing.
	result, err := c.DB("testdb").RevsDiff(t.Context(), map[string][]string{
		"doc1": {"1-aaa", "1-zzz"},
	})
	require.NoError(t, err)
	assert.NotContains(t, result, "doc1", "conflict leaf revs should not appear as missing")
}

func TestAllDocs_TotalRowsExcludesDeleted(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
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
		ID:   "doc2",
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc3",
		Data: map[string]interface{}{"x": 3},
	})
	require.NoError(t, err)

	// Delete doc1 — it becomes a tombstone in the docs bucket
	_, err = db.DeleteDocument(ctx, "doc1", rev1)
	require.NoError(t, err)

	// total_rows must reflect only live documents (2), not the tombstone
	result, err := c.DB("testdb").AllDocs(ctx, client.AllDocsQuery{})
	require.NoError(t, err)
	assert.Equal(t, 2, result.TotalRows)
	assert.Len(t, result.Rows, 2)
}
