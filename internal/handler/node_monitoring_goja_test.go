//go:build !nogoja

package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeVersionsJSEngine(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/_local/_versions", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "javascript_engine")
}
