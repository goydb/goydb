package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigAll verifies GET /_config returns all sections including defaults.
func TestConfigAll(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_config", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	assert.Contains(t, result, "couchdb")
	assert.Contains(t, result, "httpd")
	assert.Contains(t, result, "log")
	assert.Equal(t, "info", result["log"]["level"])
}

// TestConfigSection_Exists verifies GET /_config/{section} for a known section.
func TestConfigSection_Exists(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	result, err := c.GetConfigSection(t.Context(), "httpd")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", result["bind_address"])
	assert.Equal(t, "7070", result["port"])
}

// TestConfigSection_Missing verifies GET /_config/{section} returns 404 for
// an unknown section.
func TestConfigSection_Missing(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := c.GetConfigSection(t.Context(), "nonexistent")
	assert.Equal(t, http.StatusNotFound, statusCode(t, err, http.StatusOK))
}

// TestConfigKey_Exists verifies GET /_config/{section}/{key} for a known key.
func TestConfigKey_Exists(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	val, err := c.GetConfig(t.Context(), "log", "level")
	require.NoError(t, err)
	assert.Equal(t, "info", val)
}

// TestConfigKey_Missing verifies GET /_config/{section}/{key} returns 404 for
// an unknown key.
func TestConfigKey_Missing(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := c.GetConfig(t.Context(), "log", "nosuchkey")
	assert.Equal(t, http.StatusNotFound, statusCode(t, err, http.StatusOK))
}

// TestConfigKeyPut verifies PUT /_config/{section}/{key} stores the value and
// returns the previous value.
func TestConfigKeyPut(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	// First PUT: key does not exist yet — old value should reflect the
	// previous default. The PUT response body (the old value) isn't exposed
	// by the typed PutConfig, so use Raw to inspect it.
	resp, err := c.Raw(t.Context(), http.MethodPut, "/_config/log/level", strings.NewReader(`"debug"`), nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var old string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&old))
	assert.Equal(t, "info", old) // default was "info"

	// Subsequent GET should reflect the new value.
	newVal, err := c.GetConfig(t.Context(), "log", "level")
	require.NoError(t, err)
	assert.Equal(t, "debug", newVal)
}

// TestConfigKeyPut_NewSection verifies PUT creates a new section on the fly.
func TestConfigKeyPut_NewSection(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	require.NoError(t, c.PutConfig(t.Context(), "custom", "mykey", "42"))

	val, err := c.GetConfig(t.Context(), "custom", "mykey")
	require.NoError(t, err)
	assert.Equal(t, "42", val)
}

// TestConfigKeyPut_InvalidBody verifies PUT with non-string JSON returns 400.
func TestConfigKeyPut_InvalidBody(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"not":"a string"}`)
	resp, err := c.Raw(t.Context(), http.MethodPut, "/_config/log/level", body, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestConfigKeyDelete verifies DELETE /_config/{section}/{key} removes the key
// and returns the old value.
func TestConfigKeyDelete(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodDelete, "/_config/log/level", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var old string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&old))
	assert.Equal(t, "info", old)

	// Key should now be absent.
	_, err = c.GetConfig(t.Context(), "log", "level")
	assert.Equal(t, http.StatusNotFound, statusCode(t, err, http.StatusOK))
}

// TestConfigKeyDelete_Missing verifies DELETE on an unknown key returns 404.
func TestConfigKeyDelete_Missing(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	err := c.DeleteConfig(t.Context(), "log", "nosuchkey")
	assert.Equal(t, http.StatusNotFound, statusCode(t, err, http.StatusOK))
}

// TestNodeConfig verifies that the CouchDB 2.x /_node/{node}/_config/* routes
// are wired to the same store as /_config/*.
func TestNodeConfig(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	// GET /_node/nonode@nohost/_config/cors should return the cors section.
	resp, err := c.Raw(t.Context(), http.MethodGet, "/_node/nonode@nohost/_config/cors", nil, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var section map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&section))
	resp.Body.Close() //nolint:errcheck
	assert.Equal(t, "*", section["origins"])

	// PUT via node-scoped route should update the shared store.
	body := strings.NewReader(`"https://app.example.com"`)
	resp, err = c.Raw(t.Context(), http.MethodPut, "/_node/nonode@nohost/_config/cors/origins", body, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close() //nolint:errcheck

	// GET via the plain /_config route should reflect the updated value.
	val, err := c.GetConfig(t.Context(), "cors", "origins")
	require.NoError(t, err)
	assert.Equal(t, "https://app.example.com", val)
}

// TestConfigReload verifies POST /_node/{node}/_config/_reload returns ok.
func TestConfigReload(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodPost, "/_node/nonode@nohost/_config/_reload", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result["ok"])
}

// TestConfigReload_Local verifies POST /_node/_local/_config/_reload also works.
func TestConfigReload_Local(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp, err := c.Raw(t.Context(), http.MethodPost, "/_node/_local/_config/_reload", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result["ok"])
}

// TestConfigPersistence verifies that config values written via PUT survive a
// server restart by reopening the storage directory and rebuilding the router.
func TestConfigPersistence(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-config-persist-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir) //nolint:errcheck

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))
	admins := model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}}

	buildServer := func() (*client.Client, func()) {
		s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
		require.NoError(t, err)
		r := mux.NewRouter()
		err = Router{Storage: s, SessionStore: store, Admins: admins, Replication: &service.Replication{Storage: s, Logger: logger.NewNoLog()}, Logger: logger.NewNoLog()}.Build(r)
		require.NoError(t, err)
		ts := httptest.NewServer(r)
		cl, err := client.New(ts.URL)
		require.NoError(t, err)
		return cl, func() { ts.Close(); _ = s.Close() }
	}

	// --- First server instance: write a value ---
	c1, close1 := buildServer()
	require.NoError(t, c1.PutConfig(t.Context(), "cors", "origins", "https://example.com"))
	close1()

	// --- Second server instance: value must still be present ---
	c2, close2 := buildServer()
	defer close2()
	val, err := c2.GetConfig(t.Context(), "cors", "origins")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", val)
}
