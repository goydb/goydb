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
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigAll verifies GET /_config returns all sections including defaults.
func TestConfigAll(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	assert.Contains(t, result, "couchdb")
	assert.Contains(t, result, "httpd")
	assert.Contains(t, result, "log")
	assert.Equal(t, "info", result["log"]["level"])
}

// TestConfigSection_Exists verifies GET /_config/{section} for a known section.
func TestConfigSection_Exists(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_config/httpd", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, "0.0.0.0", result["bind_address"])
	assert.Equal(t, "7070", result["port"])
}

// TestConfigSection_Missing verifies GET /_config/{section} returns 404 for
// an unknown section.
func TestConfigSection_Missing(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_config/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestConfigKey_Exists verifies GET /_config/{section}/{key} for a known key.
func TestConfigKey_Exists(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_config/log/level", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var val string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&val))
	assert.Equal(t, "info", val)
}

// TestConfigKey_Missing verifies GET /_config/{section}/{key} returns 404 for
// an unknown key.
func TestConfigKey_Missing(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_config/log/nosuchkey", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestConfigKeyPut verifies PUT /_config/{section}/{key} stores the value and
// returns the previous value.
func TestConfigKeyPut(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	// First PUT: key does not exist yet — old value should be empty string.
	body := strings.NewReader(`"debug"`)
	req := httptest.NewRequest("PUT", "/_config/log/level", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var old string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&old))
	assert.Equal(t, "info", old) // default was "info"

	// Subsequent GET should reflect the new value.
	req = httptest.NewRequest("GET", "/_config/log/level", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var newVal string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&newVal))
	assert.Equal(t, "debug", newVal)
}

// TestConfigKeyPut_NewSection verifies PUT creates a new section on the fly.
func TestConfigKeyPut_NewSection(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`"42"`)
	req := httptest.NewRequest("PUT", "/_config/custom/mykey", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest("GET", "/_config/custom/mykey", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var val string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&val))
	assert.Equal(t, "42", val)
}

// TestConfigKeyPut_InvalidBody verifies PUT with non-string JSON returns 400.
func TestConfigKeyPut_InvalidBody(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"not":"a string"}`)
	req := httptest.NewRequest("PUT", "/_config/log/level", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestConfigKeyDelete verifies DELETE /_config/{section}/{key} removes the key
// and returns the old value.
func TestConfigKeyDelete(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/_config/log/level", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var old string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&old))
	assert.Equal(t, "info", old)

	// Key should now be absent.
	req = httptest.NewRequest("GET", "/_config/log/level", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestConfigKeyDelete_Missing verifies DELETE on an unknown key returns 404.
func TestConfigKeyDelete_Missing(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/_config/log/nosuchkey", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestNodeConfig verifies that the CouchDB 2.x /_node/{node}/_config/* routes
// are wired to the same store as /_config/*.
func TestNodeConfig(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	// GET /_node/nonode@nohost/_config/cors should return the cors section.
	req := httptest.NewRequest("GET", "/_node/nonode@nohost/_config/cors", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var section map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&section))
	assert.Equal(t, "*", section["origins"])

	// PUT via node-scoped route should update the shared store.
	body := strings.NewReader(`"https://app.example.com"`)
	req = httptest.NewRequest("PUT", "/_node/nonode@nohost/_config/cors/origins", body)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// GET via the plain /_config route should reflect the updated value.
	req = httptest.NewRequest("GET", "/_config/cors/origins", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var val string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&val))
	assert.Equal(t, "https://app.example.com", val)
}

// TestConfigPersistence verifies that config values written via PUT survive a
// server restart by reopening the storage directory and rebuilding the router.
func TestConfigPersistence(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-config-persist-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir) //nolint:errcheck

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))
	admins := model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}}

	buildRouter := func() (http.Handler, func()) {
		s, err := storage.Open(dir)
		require.NoError(t, err)
		r := mux.NewRouter()
		err = Router{Storage: s, SessionStore: store, Admins: admins, ReplicationService: &controller.ReplicationService{Storage: s}}.Build(r)
		require.NoError(t, err)
		return r, func() { _ = s.Close() }
	}

	// --- First server instance: write a value ---
	router1, close1 := buildRouter()
	body := strings.NewReader(`"https://example.com"`)
	req := httptest.NewRequest("PUT", "/_config/cors/origins", body)
	w := httptest.NewRecorder()
	router1.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	close1()

	// --- Second server instance: value must still be present ---
	router2, close2 := buildRouter()
	defer close2()
	req = httptest.NewRequest("GET", "/_config/cors/origins", nil)
	w = httptest.NewRecorder()
	router2.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var val string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&val))
	assert.Equal(t, "https://example.com", val)
}
