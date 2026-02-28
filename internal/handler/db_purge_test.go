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

func TestPurge_Basic(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body := strings.NewReader(`{"doc1":["1-abc"],"doc2":["1-def","2-ghi"]}`)
	req := httptest.NewRequest("POST", "/testdb/_purge", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		PurgeSeq interface{}         `json:"purge_seq"`
		Purged   map[string][]string `json:"purged"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Len(t, result.Purged, 2)
	assert.Equal(t, []string{"1-abc"}, result.Purged["doc1"])
	assert.Equal(t, []string{"1-def", "2-ghi"}, result.Purged["doc2"])
}

func TestPurgedInfosLimit_Get(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/testdb/_purged_infos_limit", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var limit int
	require.NoError(t, json.NewDecoder(w.Body).Decode(&limit))
	assert.Equal(t, 1000, limit)
}

func TestPurgedInfosLimit_Put(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body := strings.NewReader(`500`)
	req := httptest.NewRequest("PUT", "/testdb/_purged_infos_limit", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result["ok"])
}
