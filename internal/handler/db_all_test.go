package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getAllDbs(t *testing.T, c *client.Client, query string) (code int, result []string) {
	t.Helper()
	path := "/_all_dbs"
	if query != "" {
		path += "?" + query
	}
	resp, err := c.Raw(t.Context(), http.MethodGet, path, nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	result = []string{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func createDbs(t *testing.T, c *client.Client, names ...string) {
	t.Helper()
	for _, name := range names {
		require.NoError(t, c.DB(name).Create(t.Context()))
	}
}

// createRegularUser creates the _users database and a regular user document
// with the given username and password.
func createRegularUser(t *testing.T, c *client.Client, username, password string) {
	t.Helper()
	// Create _users database.
	require.NoError(t, c.DB("_users").Create(t.Context()))

	// Create user document with plaintext password (handler hashes it).
	code, _ := putDoc(t, c, "/_users/org.couchdb.user:"+username, map[string]interface{}{
		"_id":      "org.couchdb.user:" + username,
		"name":     username,
		"type":     "user",
		"roles":    []string{},
		"password": password,
	})
	require.Equal(t, http.StatusCreated, code)
}

// setupMultiAuthTest is like setupClientTest, but returns the server's base
// URL instead of a pre-authenticated client, so a test can create several
// pkg/client.Client instances against it with different (or no) credentials.
func setupMultiAuthTest(t *testing.T) (*storage.Storage, string, func()) {
	t.Helper()

	log := logger.NewNoLog()
	s, err := storage.Open(t.TempDir(), storage.WithLogger(log))
	require.NoError(t, err)

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	taskCtx, cancelTasks := context.WithCancel(context.Background())
	tc := controller.Task{Storage: s, Logger: log}
	go tc.Run(taskCtx)

	ts := httptest.NewServer(r)

	return s, ts.URL, func() {
		cancelTasks()
		ts.Close()
		_ = s.Close()
	}
}

func TestAllDbs_Basic(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, c, "")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, result)
}

func TestAllDbs_Descending(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, c, "descending=true")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, result)
}

func TestAllDbs_Limit(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, c, "limit=2")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"alpha", "bravo"}, result)
}

func TestAllDbs_Skip(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, c, "skip=1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

func TestAllDbs_StartKey(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, c, `startkey="bravo"`)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

func TestAllDbs_EndKey(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, c, `endkey="bravo"`)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"alpha", "bravo"}, result)
}

func TestAllDbs_StartKeyEndKeyCombined(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie", "delta")

	code, result := getAllDbs(t, c, `startkey="bravo"&endkey="charlie"`)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

func TestAllDbs_SkipAndLimit(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, c, "alpha", "bravo", "charlie", "delta")

	code, result := getAllDbs(t, c, "skip=1&limit=2")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

func TestAllDbs_AdminOnly_Default(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	admin, err := client.New(baseURL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	createRegularUser(t, admin, "alice", "alicepass")

	// Unauthenticated request should get 401.
	anon, err := client.New(baseURL)
	require.NoError(t, err)
	_, err = anon.AllDBs(t.Context())
	assert.Equal(t, http.StatusUnauthorized, statusCode(t, err, http.StatusOK))

	// Regular user should get 401 (not a server admin).
	alice, err := client.New(baseURL, client.WithBasicAuth("alice", "alicepass"))
	require.NoError(t, err)
	_, err = alice.AllDBs(t.Context())
	assert.Equal(t, http.StatusUnauthorized, statusCode(t, err, http.StatusOK))
}

func TestAllDbs_AdminOnly_False(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	admin, err := client.New(baseURL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	createRegularUser(t, admin, "alice", "alicepass")
	createDbs(t, admin, "mydb")

	// Set admin_only_all_dbs to false.
	require.NoError(t, admin.PutConfig(t.Context(), "chttpd", "admin_only_all_dbs", "false"))

	// Regular user should now be able to access _all_dbs.
	alice, err := client.New(baseURL, client.WithBasicAuth("alice", "alicepass"))
	require.NoError(t, err)
	dbs, err := alice.AllDBs(t.Context())
	require.NoError(t, err)
	assert.Contains(t, dbs, "mydb")
}
