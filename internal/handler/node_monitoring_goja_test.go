//go:build !nogoja

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeVersionsJSEngine(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_node/_local/_versions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Contains(t, result, "javascript_engine")
}
