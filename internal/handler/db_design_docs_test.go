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

func TestDesignDocs_PostKeys(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create design docs
	putDoc(t, router, "/testdb/_design/view1", map[string]interface{}{"views": map[string]interface{}{}})
	putDoc(t, router, "/testdb/_design/view2", map[string]interface{}{"views": map[string]interface{}{}})

	body, _ := json.Marshal(map[string]interface{}{
		"keys": []string{"_design/view1", "_design/view2"},
	})
	req := httptest.NewRequest("POST", "/testdb/_design_docs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	rows := result["rows"].([]interface{})
	assert.Len(t, rows, 2)
}

func TestDesignDocs_IncludeDocs(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDoc(t, router, "/testdb/_design/myview", map[string]interface{}{
		"views": map[string]interface{}{},
	})

	req := httptest.NewRequest("GET", "/testdb/_design_docs?include_docs=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	rows := result["rows"].([]interface{})
	require.Len(t, rows, 1)
	row := rows[0].(map[string]interface{})
	assert.NotNil(t, row["doc"])
}

func TestDesignDocs_UpdateSeq(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/testdb/_design_docs?update_seq=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.NotNil(t, result["update_seq"])
}
