package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
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

// setupBrokenDBTest writes a .sqlite3 file to disk *before* opening Storage
// (which never registers a "sqlite" engine), reproducing "a database file
// this binary can't open" without needing the real sqlite engine / build tag.
func setupBrokenDBTest(t *testing.T) (*client.Client, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-broken-db-handler-test-*")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path.Join(dir, "broken.sqlite3"), []byte("not a real db"), 0o600))

	log := logger.NewNoLog()
	s, err := storage.Open(dir, storage.WithLogger(log))
	require.NoError(t, err, "an unopenable database file must not fail Storage.Open")

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	ts := httptest.NewServer(r)

	c, err := client.New(ts.URL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)

	return c, func() {
		ts.Close()
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestBrokenDatabase_ListedInAllDbs(t *testing.T) {
	c, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	names, err := c.AllDBs(t.Context())
	require.NoError(t, err)
	assert.Contains(t, names, "broken")
}

func TestBrokenDatabase_HeadReturns500(t *testing.T) {
	c, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	_, err := c.DB("broken").Exists(t.Context())
	require.Error(t, err)
	assert.Equal(t, http.StatusInternalServerError, statusCode(t, err, http.StatusOK))
}

func TestBrokenDatabase_DocGetReturns500WithClearMessage(t *testing.T) {
	c, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	resp, err := c.DB("broken").Raw(t.Context(), http.MethodGet, "/doc1", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "could not be opened")
}

func TestBrokenDatabase_CreateReturnsConflict(t *testing.T) {
	c, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	err := c.DB("broken").Create(t.Context())
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, statusCode(t, err, http.StatusCreated))
}

func TestBrokenDatabase_DeleteRemovesItAndFreesTheName(t *testing.T) {
	c, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	require.NoError(t, c.DB("broken").Destroy(t.Context()))

	names, err := c.AllDBs(t.Context())
	require.NoError(t, err)
	assert.NotContains(t, names, "broken")

	// Name must be free to recreate now that the broken file is gone.
	require.NoError(t, c.DB("broken").Create(t.Context()))
}
