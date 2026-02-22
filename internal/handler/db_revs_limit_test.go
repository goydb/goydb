package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevsLimitGet_Default(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/testdb/_revs_limit", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var limit int
	require.NoError(t, json.NewDecoder(w.Body).Decode(&limit))
	assert.Equal(t, 1000, limit)
}

func TestRevsLimitPut_Valid(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body, _ := json.Marshal(500)
	req := httptest.NewRequest("PUT", "/testdb/_revs_limit", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result["ok"])
}

func TestRevsLimitGet_AfterPut(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// PUT 500
	body, _ := json.Marshal(500)
	req := httptest.NewRequest("PUT", "/testdb/_revs_limit", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// GET should now return 500
	req = httptest.NewRequest("GET", "/testdb/_revs_limit", nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var limit int
	require.NoError(t, json.NewDecoder(w.Body).Decode(&limit))
	assert.Equal(t, 500, limit)
}

func TestRevsLimitPut_InvalidBody(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	tests := []struct {
		name string
		body []byte
	}{
		{"zero", []byte("0")},
		{"negative", []byte("-5")},
		{"not a number", []byte(`"hello"`)},
		{"empty", []byte("")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/testdb/_revs_limit", bytes.NewReader(tc.body))
			req.SetBasicAuth("admin", "secret")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}
