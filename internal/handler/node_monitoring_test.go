package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeStats(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/_local/_stats", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "couchdb")
}

func TestNodeSystem(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/_local/_system", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "memory")
	assert.Contains(t, result, "run_queue")
}

func TestNodeRestart(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodPost, "/_node/_local/_restart", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result["ok"])
}

func TestNodeVersions(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/_local/_versions", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "collation_driver")
}

func TestNodeSmooshStatus(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/_local/_smoosh/status", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "channels")
}
