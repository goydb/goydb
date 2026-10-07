package handler

import (
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
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBrokenDBTest writes a .sqlite3 file to disk *before* opening Storage
// (which never registers a "sqlite" engine), reproducing "a database file
// this binary can't open" without needing the real sqlite engine / build tag.
func setupBrokenDBTest(t *testing.T) (*mux.Router, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-broken-db-handler-test-*")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path.Join(dir, "broken.sqlite3"), []byte("not a real db"), 0o600))

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err, "an unopenable database file must not fail Storage.Open")

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	return r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestBrokenDatabase_ListedInAllDbs(t *testing.T) {
	router, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	code, names := getAllDbs(t, router, "")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, names, "broken")
}

func TestBrokenDatabase_HeadReturns500(t *testing.T) {
	router, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	req := httptest.NewRequest("HEAD", "/broken", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestBrokenDatabase_DocGetReturns500WithClearMessage(t *testing.T) {
	router, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/broken/doc1", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "could not be opened")
}

func TestBrokenDatabase_CreateReturnsConflict(t *testing.T) {
	router, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	req := httptest.NewRequest("PUT", "/broken", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestBrokenDatabase_DeleteRemovesItAndFreesTheName(t *testing.T) {
	router, cleanup := setupBrokenDBTest(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/broken", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	code, names := getAllDbs(t, router, "")
	assert.Equal(t, http.StatusOK, code)
	assert.NotContains(t, names, "broken")

	// Name must be free to recreate now that the broken file is gone.
	req = httptest.NewRequest("PUT", "/broken", nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}
