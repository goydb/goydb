//go:build !nogoja

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewQueries_Basic(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob", "carol"})

	body := strings.NewReader(`{"queries":[{"reduce":"false","limit":"2"},{"reduce":"false","skip":"1"}]}`)
	req := httptest.NewRequest("POST", "/testdb/_design/users/_view/by_name/queries", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Results []ViewResponse `json:"results"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result.Results, 2)

	// First query: limit=2
	assert.Len(t, result.Results[0].Rows, 2)

	// Second query: skip=1, so 2 rows
	assert.Len(t, result.Results[1].Rows, 2)
}

func TestViewQueries_EmptyQueries(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice"})

	body := strings.NewReader(`{"queries":[]}`)
	req := httptest.NewRequest("POST", "/testdb/_design/users/_view/by_name/queries", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Results []json.RawMessage `json:"results"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Empty(t, result.Results)
}
