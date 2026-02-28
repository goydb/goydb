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

func TestNodeInfo(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_node/nonode@nohost", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, "nonode@nohost", result["name"])
}

func TestNodeInfo_Local(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_node/_local", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, "nonode@nohost", result["name"])
}

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

func TestClusterSetupPost_AllActions(t *testing.T) {
	actions := []string{
		"enable_single_node",
		"finish_cluster",
		"enable_cluster",
		"add_node",
		"remove_node",
	}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			_, router, cleanup := setupRevsDiffTest(t)
			defer cleanup()

			body := strings.NewReader(`{"action":"` + action + `"}`)
			req := httptest.NewRequest("POST", "/_cluster_setup", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)

			var result map[string]interface{}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
			assert.Equal(t, true, result["ok"])
		})
	}
}

func TestClusterSetupPost_MissingAction(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("POST", "/_cluster_setup", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestClusterSetupPost_UnknownAction(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"action":"do_magic"}`)
	req := httptest.NewRequest("POST", "/_cluster_setup", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
