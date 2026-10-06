package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
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

// setupDBCreateEngineTest is like setupRevsDiffTest but also returns the
// *ConfigStore, so tests can set [couchdb] default_engine before creating a
// database.
func setupDBCreateEngineTest(t *testing.T) (*ConfigStore, *mux.Router, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-handler-test-*")
	require.NoError(t, err)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	cs := NewConfigStore("", logger.NewNoLog())
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		Config:       cs,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	return cs, r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestDBCreate_ExplicitEngineParamUsed(t *testing.T) {
	_, router, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	req := httptest.NewRequest("PUT", "/newdb?engine=bbolt", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestDBCreate_UnknownEngineParamReturns400(t *testing.T) {
	_, router, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	req := httptest.NewRequest("PUT", "/newdb?engine=does-not-exist", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDBCreate_DefaultEngineConfigAppliedWhenNoQueryParam(t *testing.T) {
	cs, router, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	cs.Set("couchdb", "default_engine", "does-not-exist")

	req := httptest.NewRequest("PUT", "/newdb", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// The configured default is bogus and no ?engine= override was given —
	// must fail exactly as an explicit bad engine would, proving the
	// [couchdb] default_engine config value actually reaches CreateDatabase.
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDBCreate_QueryParamOverridesConfigDefault(t *testing.T) {
	cs, router, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	cs.Set("couchdb", "default_engine", "does-not-exist")

	req := httptest.NewRequest("PUT", "/newdb?engine=bbolt", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// An explicit ?engine= must win over a (here, deliberately bad)
	// configured default.
	assert.Equal(t, http.StatusCreated, w.Code)
}
