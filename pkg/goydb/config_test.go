package goydb

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig(t *testing.T) *Config {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-config-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return &Config{
		DatabaseDir:     dir,
		EnablePublicDir: false,
		CookieSecret:    strings.Repeat("ab", 32),
		Aministrators:   "admin:secret",
	}
}

func TestBuildDatabase_RouteHookRegistersReachableRoute(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.RouteHooks = []RouteHook{
		func(r *mux.Router, gdb *Goydb) error {
			r.Methods("GET").Path("/_hooktest/a/b/c").Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("hooked"))
			}))
			return nil
		},
	}

	gdb, err := cfg.BuildDatabase()
	require.NoError(t, err)
	defer gdb.Close() //nolint:errcheck

	ts := httptest.NewServer(gdb.Handler)
	defer ts.Close()
	c, err := client.New(ts.URL)
	require.NoError(t, err)

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_hooktest/a/b/c", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hooked", string(body))
}

func TestBuildDatabase_RouteHookErrorFailsBuild(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.RouteHooks = []RouteHook{
		func(r *mux.Router, gdb *Goydb) error {
			return assert.AnError
		},
	}

	_, err := cfg.BuildDatabase()
	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestBuildDatabase_RouteHookWorksWithCORSEnabled(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.RouteHooks = []RouteHook{
		func(r *mux.Router, gdb *Goydb) error {
			r.Methods("GET").Path("/_hooktest/a/b/c").Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			return nil
		},
	}

	// Pre-seed enable_cors=true on disk before BuildDatabase ever opens
	// storage, rather than opening two Storage instances against the same
	// bbolt files concurrently (which deadlocks on bbolt's file lock).
	configPath := filepath.Join(cfg.DatabaseDir, "_config.json")
	seed := handler.NewConfigStore(configPath, nil)
	seed.Set("httpd", "enable_cors", "true")

	gdb, err := cfg.BuildDatabase()
	require.NoError(t, err)
	defer gdb.Close() //nolint:errcheck

	ts := httptest.NewServer(gdb.Handler)
	defer ts.Close()
	c, err := client.New(ts.URL)
	require.NoError(t, err)

	resp, err := c.Raw(t.Context(), http.MethodGet, "/_hooktest/a/b/c", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
