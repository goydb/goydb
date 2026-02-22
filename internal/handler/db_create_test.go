package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBCreate_ReturnsOkTrue(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("PUT", "/newdb", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
}

func TestDBCreate_ConflictIfExists(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("PUT", "/newdb", nil)
	req.SetBasicAuth("admin", "secret")
	router.ServeHTTP(httptest.NewRecorder(), req)

	// second PUT to same db
	req = httptest.NewRequest("PUT", "/newdb", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}
