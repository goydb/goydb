//go:build !nosearch

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

func TestSearchCleanup(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/testdb/_search_cleanup", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result["ok"])
}

func TestNouveauCleanup(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/testdb/_nouveau_cleanup", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result["ok"])
}

func TestSearchAnalyze(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"analyzer":"standard","text":"hello world foo"}`)
	req := httptest.NewRequest("POST", "/_search_analyze", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Tokens []string `json:"tokens"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, []string{"hello", "world", "foo"}, result.Tokens)
}

func TestNouveauAnalyze(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"analyzer":"standard","text":"one two"}`)
	req := httptest.NewRequest("POST", "/_nouveau_analyze", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Tokens []string `json:"tokens"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, []string{"one", "two"}, result.Tokens)
}
