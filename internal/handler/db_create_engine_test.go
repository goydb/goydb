package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/service"
	pkgclient "github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDBCreateEngineTest is like setupClientTest but also returns the
// *ConfigStore, so tests can set [couchdb] default_engine before creating a
// database.
func setupDBCreateEngineTest(t *testing.T) (*ConfigStore, *pkgclient.Client, func()) {
	t.Helper()
	log := logger.NewNoLog()
	s, err := storage.Open(t.TempDir(), storage.WithLogger(log))
	require.NoError(t, err)

	cs := NewConfigStore("", log)

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		Config:       cs,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	ts := httptest.NewServer(r)

	c, err := pkgclient.New(ts.URL, pkgclient.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)

	return cs, c, func() {
		ts.Close()
		_ = s.Close()
	}
}

func TestDBCreate_ExplicitEngineParamUsed(t *testing.T) {
	_, c, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	require.NoError(t, c.DB("newdb").Create(t.Context(), map[string]string{"engine": "bbolt"}))
}

func TestDBCreate_UnknownEngineParamReturns400(t *testing.T) {
	_, c, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	err := c.DB("newdb").Create(t.Context(), map[string]string{"engine": "does-not-exist"})
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, statusCode(t, err, http.StatusCreated))
}

func TestDBCreate_DefaultEngineConfigAppliedWhenNoQueryParam(t *testing.T) {
	cs, c, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	cs.Set("couchdb", "default_engine", "does-not-exist")

	// The configured default is bogus and no ?engine= override was given —
	// must fail exactly as an explicit bad engine would, proving the
	// [couchdb] default_engine config value actually reaches CreateDatabase.
	err := c.DB("newdb").Create(t.Context())
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, statusCode(t, err, http.StatusCreated))
}

func TestDBCreate_QueryParamOverridesConfigDefault(t *testing.T) {
	cs, c, cleanup := setupDBCreateEngineTest(t)
	defer cleanup()

	cs.Set("couchdb", "default_engine", "does-not-exist")

	// An explicit ?engine= must win over a (here, deliberately bad)
	// configured default.
	require.NoError(t, c.DB("newdb").Create(t.Context(), map[string]string{"engine": "bbolt"}))
}
