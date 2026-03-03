package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeStats(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_node/_local/_stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Contains(t, result, "couchdb")
}

func TestNodeSystem(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_node/_local/_system", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Contains(t, result, "memory")
	assert.Contains(t, result, "run_queue")
}

func TestNodeRestart(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/_node/_local/_restart", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result["ok"])
}

func TestNodeVersions(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_node/_local/_versions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Contains(t, result, "collation_driver")
}

func TestNodeSmooshStatus(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_node/_local/_smoosh/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Contains(t, result, "channels")
}
