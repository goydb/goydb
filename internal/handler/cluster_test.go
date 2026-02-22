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

func TestMembership(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_membership", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		AllNodes     []string `json:"all_nodes"`
		ClusterNodes []string `json:"cluster_nodes"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, []string{"nonode@nohost"}, result.AllNodes)
	assert.Equal(t, []string{"nonode@nohost"}, result.ClusterNodes)
}

func TestClusterSetupGet(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_cluster_setup", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, "single_node_enabled", result["state"])
}

func TestClusterSetupPost(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"action":"enable_single_node"}`)
	req := httptest.NewRequest("POST", "/_cluster_setup", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
}
