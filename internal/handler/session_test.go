package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionGet_Anonymous(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_session", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	userCtx, ok := result["userCtx"].(map[string]interface{})
	require.True(t, ok, "userCtx must be an object")
	assert.Nil(t, userCtx["name"], "anonymous name must be null")

	roles, ok := userCtx["roles"].([]interface{})
	require.True(t, ok, "roles must be an array")
	assert.Empty(t, roles)

	info, ok := result["info"].(map[string]interface{})
	require.True(t, ok, "info must be an object")
	assert.Equal(t, "_users", info["authentication_db"])
}

func TestSessionGet_BasicAuth(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_session", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	userCtx, ok := result["userCtx"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "admin", userCtx["name"])

	info, ok := result["info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "default", info["authenticated"])
	assert.Equal(t, "_users", info["authentication_db"])
}

func TestSessionPost_FormBody(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	form := url.Values{"name": {"admin"}, "password": {"secret"}}
	req := httptest.NewRequest("POST", "/_session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Result().Cookies(), "Set-Cookie must be present")

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "admin", result["name"])
	_, hasRoles := result["roles"]
	assert.True(t, hasRoles, "roles field must be present")
	_, hasUserCtx := result["userCtx"]
	assert.False(t, hasUserCtx, "userCtx must NOT be a nested field")
}

func TestSessionPost_JSONBody(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{"name": "admin", "password": "secret"})
	req := httptest.NewRequest("POST", "/_session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Result().Cookies(), "Set-Cookie must be present")

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "admin", result["name"])
	_, hasRoles := result["roles"]
	assert.True(t, hasRoles)
	_, hasUserCtx := result["userCtx"]
	assert.False(t, hasUserCtx, "userCtx must NOT be a nested field")
}

func TestSessionPost_WrongPassword(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	form := url.Values{"name": {"admin"}, "password": {"wrong"}}
	req := httptest.NewRequest("POST", "/_session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSessionDelete_NotLoggedIn(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/_session", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
}
