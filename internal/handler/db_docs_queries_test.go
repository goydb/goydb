package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllDocsQueries_Basic(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, id := range []string{"a", "b", "c"} {
		_, err := db.PutDocument(ctx, &model.Document{
			ID:   id,
			Data: map[string]interface{}{"name": id},
		})
		require.NoError(t, err)
	}

	body := strings.NewReader(`{"queries":[{"limit":1},{"startkey":"b"}]}`)
	req := httptest.NewRequest("POST", "/testdb/_all_docs/queries", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Results []AllDocsResponse `json:"results"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result.Results, 2)

	// First query: limit=1
	assert.Len(t, result.Results[0].Rows, 1)

	// Second query: startkey=b → should return b and c
	assert.GreaterOrEqual(t, len(result.Results[1].Rows), 2)
}

func TestAllDocsQueries_WithKeys(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"val": 1},
	})
	require.NoError(t, err)

	body := strings.NewReader(`{"queries":[{"keys":["doc1","nonexistent"]}]}`)
	req := httptest.NewRequest("POST", "/testdb/_all_docs/queries", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Results []AllDocsResponse `json:"results"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result.Results, 1)
	require.Len(t, result.Results[0].Rows, 2)

	assert.Equal(t, "doc1", result.Results[0].Rows[0].ID)
	assert.Equal(t, "not_found", result.Results[0].Rows[1].Error)
}

func TestDesignDocsQueries_Basic(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a design doc and a regular doc
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "_design/myview",
		Data: map[string]interface{}{"views": map[string]interface{}{}},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "regular_doc",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	body := strings.NewReader(`{"queries":[{}]}`)
	req := httptest.NewRequest("POST", "/testdb/_design_docs/queries", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Results []AllDocsResponse `json:"results"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result.Results, 1)

	// Should only return design docs, not regular docs
	for _, row := range result.Results[0].Rows {
		assert.True(t, strings.HasPrefix(row.ID, "_design/"), "expected design doc, got: %s", row.ID)
	}
	assert.GreaterOrEqual(t, len(result.Results[0].Rows), 1)
}
