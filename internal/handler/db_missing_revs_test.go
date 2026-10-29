package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingRevs_AllMissing(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	result, err := c.DB("testdb").MissingRevs(t.Context(), map[string][]string{
		"doc1": {"1-a"},
		"doc2": {"1-b"},
	})
	require.NoError(t, err)

	missingRevs, _ := result["missing_revs"].(map[string]interface{})
	require.NotNil(t, missingRevs)
	assert.Contains(t, missingRevs, "doc1")
	assert.Contains(t, missingRevs, "doc2")
}

func TestMissingRevs_CurrentRevPresent(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	result, err := c.DB("testdb").MissingRevs(ctx, map[string][]string{
		"doc1": {rev},
	})
	require.NoError(t, err)

	missingRevs, _ := result["missing_revs"].(map[string]interface{})
	assert.NotContains(t, missingRevs, "doc1")
}

func TestMissingRevs_AncestorRevNotMissing(t *testing.T) {
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

	// rev1 is now an ancestor — must not appear in missing_revs.
	result, err := c.DB("testdb").MissingRevs(ctx, map[string][]string{
		"doc1": {rev1},
	})
	require.NoError(t, err)

	missingRevs, _ := result["missing_revs"].(map[string]interface{})
	assert.NotContains(t, missingRevs, "doc1", "ancestor rev should not be reported as missing")
}

func TestMissingRevs_ConflictLeafNotMissing(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
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

	result, err := c.DB("testdb").MissingRevs(ctx, map[string][]string{
		"doc1": {"1-aaa", "1-zzz"},
	})
	require.NoError(t, err)

	missingRevs, _ := result["missing_revs"].(map[string]interface{})
	assert.NotContains(t, missingRevs, "doc1", "conflict leaf revs should not appear as missing")
}

func TestMissingRevs_InvalidJSON(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	resp, err := c.DB("testdb").Raw(t.Context(), http.MethodPost, "/_missing_revs", strings.NewReader("not json"), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
