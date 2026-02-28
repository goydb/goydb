package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShards_Basic(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/testdb/_shards", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Shards map[string][]string `json:"shards"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Contains(t, result.Shards, "00000000-ffffffff")
	assert.Equal(t, []string{"nonode@nohost"}, result.Shards["00000000-ffffffff"])
}

func TestShardsDoc(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/testdb/_shards/mydoc", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Range string   `json:"range"`
		Nodes []string `json:"nodes"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, "00000000-ffffffff", result.Range)
	assert.Equal(t, []string{"nonode@nohost"}, result.Nodes)
}

func TestSyncShards(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/testdb/_sync_shards", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result["ok"])
}
