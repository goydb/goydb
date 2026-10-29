package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeInfo(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/nonode@nohost", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "nonode@nohost", result["name"])
}

func TestNodeInfo_Local(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/_local", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "nonode@nohost", result["name"])
}

func TestMembership(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	result, err := c.Membership(t.Context())
	require.NoError(t, err)

	allNodes, _ := result["all_nodes"].([]interface{})
	clusterNodes, _ := result["cluster_nodes"].([]interface{})
	assert.Equal(t, []interface{}{"nonode@nohost"}, allNodes)
	assert.Equal(t, []interface{}{"nonode@nohost"}, clusterNodes)
}

func TestClusterSetupGet(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_cluster_setup", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
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
			_, c, cleanup := setupRevsDiffTest(t)
			defer cleanup()

			body := strings.NewReader(`{"action":"` + action + `"}`)
			resp, err := c.Raw(t.Context(), http.MethodPost, "/_cluster_setup", body, map[string]string{"Content-Type": "application/json"})
			require.NoError(t, err)
			defer resp.Body.Close() //nolint:errcheck

			require.Equal(t, http.StatusOK, resp.StatusCode)

			var result map[string]interface{}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
			assert.Equal(t, true, result["ok"])
		})
	}
}

func TestClusterSetupPost_MissingAction(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{}`)
	resp, err := c.Raw(t.Context(), http.MethodPost, "/_cluster_setup", body, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestClusterSetupPost_UnknownAction(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"action":"do_magic"}`)
	resp, err := c.Raw(t.Context(), http.MethodPost, "/_cluster_setup", body, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
