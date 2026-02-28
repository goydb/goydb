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

func TestExplain_Basic(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body := strings.NewReader(`{"selector":{"name":"Alice"}}`)
	req := httptest.NewRequest("POST", "/testdb/_explain", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	assert.Equal(t, "testdb", result["dbname"])
	assert.NotNil(t, result["index"])
	assert.NotNil(t, result["selector"])
	assert.NotNil(t, result["opts"])

	idx := result["index"].(map[string]interface{})
	assert.Equal(t, "_all_docs", idx["name"])
	assert.Equal(t, "special", idx["type"])
}

func TestExplain_WithOptions(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	body := strings.NewReader(`{"selector":{"age":{"$gt":21}},"limit":10,"skip":5,"fields":["name","age"]}`)
	req := httptest.NewRequest("POST", "/testdb/_explain", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	assert.Equal(t, float64(10), result["limit"])
	assert.Equal(t, float64(5), result["skip"])

	fields := result["fields"].([]interface{})
	assert.Contains(t, fields, "name")
	assert.Contains(t, fields, "age")
}

func TestExplain_DBNotFound(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"selector":{"name":"test"}}`)
	req := httptest.NewRequest("POST", "/nonexistent/_explain", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
