//go:build !nogoja

package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewQueries_Basic(t *testing.T) {
	s, c, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, c, "testdb", []string{"alice", "bob", "carol"})

	body := strings.NewReader(`{"queries":[{"reduce":"false","limit":"2"},{"reduce":"false","skip":"1"}]}`)
	resp, err := c.Raw(t.Context(), http.MethodPost, "/testdb/_design/users/_view/by_name/queries", body, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Results []ViewResponse `json:"results"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Results, 2)

	// First query: limit=2
	assert.Len(t, result.Results[0].Rows, 2)

	// Second query: skip=1, so 2 rows
	assert.Len(t, result.Results[1].Rows, 2)
}

func TestViewQueries_EmptyQueries(t *testing.T) {
	s, c, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, c, "testdb", []string{"alice"})

	body := strings.NewReader(`{"queries":[]}`)
	resp, err := c.Raw(t.Context(), http.MethodPost, "/testdb/_design/users/_view/by_name/queries", body, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Results []json.RawMessage `json:"results"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Results)
}
