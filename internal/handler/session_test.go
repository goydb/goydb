package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionGet_Anonymous(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	anon, err := client.New(baseURL)
	require.NoError(t, err)

	result, err := anon.GetSession(t.Context())
	require.NoError(t, err)

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
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	result, err := c.GetSession(t.Context())
	require.NoError(t, err)

	userCtx, ok := result["userCtx"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "admin", userCtx["name"])

	info, ok := result["info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "default", info["authenticated"])
	assert.Equal(t, "_users", info["authentication_db"])
}

func TestSessionPost_FormBody(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	anon, err := client.New(baseURL)
	require.NoError(t, err)

	form := url.Values{"name": {"admin"}, "password": {"secret"}}
	resp, err := anon.Raw(t.Context(), http.MethodPost, "/_session", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Values("Set-Cookie"), "Set-Cookie must be present")

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "admin", result["name"])
	_, hasRoles := result["roles"]
	assert.True(t, hasRoles, "roles field must be present")
	_, hasUserCtx := result["userCtx"]
	assert.False(t, hasUserCtx, "userCtx must NOT be a nested field")
}

func TestSessionPost_JSONBody(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	anon, err := client.New(baseURL)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{"name": "admin", "password": "secret"})
	resp, err := anon.Raw(t.Context(), http.MethodPost, "/_session", bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Values("Set-Cookie"), "Set-Cookie must be present")

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "admin", result["name"])
	_, hasRoles := result["roles"]
	assert.True(t, hasRoles)
	_, hasUserCtx := result["userCtx"]
	assert.False(t, hasUserCtx, "userCtx must NOT be a nested field")
}

func TestSessionPost_WrongPassword(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	anon, err := client.New(baseURL)
	require.NoError(t, err)

	form := url.Values{"name": {"admin"}, "password": {"wrong"}}
	resp, err := anon.Raw(t.Context(), http.MethodPost, "/_session", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSessionDelete_NotLoggedIn(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	anon, err := client.New(baseURL)
	require.NoError(t, err)

	err = anon.DeleteSession(t.Context())
	require.NoError(t, err)
}
