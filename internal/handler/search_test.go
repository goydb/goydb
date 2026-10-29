//go:build !nosearch

package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCleanup(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	resp, err := c.DB("testdb").Raw(t.Context(), http.MethodPost, "/_search_cleanup", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result["ok"])
}

func TestNouveauCleanup(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	resp, err := c.DB("testdb").Raw(t.Context(), http.MethodPost, "/_nouveau_cleanup", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result["ok"])
}

func TestSearchAnalyze(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"analyzer":"standard","text":"hello world foo"}`)
	resp, err := c.Raw(t.Context(), http.MethodPost, "/_search_analyze", body, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Tokens []string `json:"tokens"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, []string{"hello", "world", "foo"}, result.Tokens)
}

func TestNouveauAnalyze(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"analyzer":"standard","text":"one two"}`)
	resp, err := c.Raw(t.Context(), http.MethodPost, "/_nouveau_analyze", body, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Tokens []string `json:"tokens"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, []string{"one", "two"}, result.Tokens)
}
