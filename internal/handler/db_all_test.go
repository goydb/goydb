package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getAllDbs(t *testing.T, router http.Handler, query string) (code int, result []string) {
	t.Helper()
	path := "/_all_dbs"
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest("GET", path, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = []string{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

func createDbs(t *testing.T, router http.Handler, names ...string) {
	t.Helper()
	for _, name := range names {
		req := httptest.NewRequest("PUT", "/"+name, nil)
		req.SetBasicAuth("admin", "secret")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}
}

func TestAllDbs_Basic(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, router, "")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, result)
}

func TestAllDbs_Descending(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, router, "descending=true")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, result)
}

func TestAllDbs_Limit(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, router, "limit=2")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"alpha", "bravo"}, result)
}

func TestAllDbs_Skip(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, router, "skip=1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

func TestAllDbs_StartKey(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, router, `startkey="bravo"`)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

func TestAllDbs_EndKey(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie")

	code, result := getAllDbs(t, router, `endkey="bravo"`)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"alpha", "bravo"}, result)
}

func TestAllDbs_StartKeyEndKeyCombined(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie", "delta")

	code, result := getAllDbs(t, router, `startkey="bravo"&endkey="charlie"`)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

func TestAllDbs_SkipAndLimit(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createDbs(t, router, "alpha", "bravo", "charlie", "delta")

	code, result := getAllDbs(t, router, "skip=1&limit=2")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{"bravo", "charlie"}, result)
}

// createRegularUser creates the _users database and a regular user document
// with the given username and password.
func createRegularUser(t *testing.T, router http.Handler, username, password string) {
	t.Helper()
	// Create _users database.
	req := httptest.NewRequest("PUT", "/_users", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Create user document with plaintext password (handler hashes it).
	userDoc, _ := json.Marshal(map[string]interface{}{
		"_id":      "org.couchdb.user:" + username,
		"name":     username,
		"type":     "user",
		"roles":    []string{},
		"password": password,
	})
	req = httptest.NewRequest("PUT", "/_users/org.couchdb.user:"+username, bytes.NewReader(userDoc))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
}

func TestAllDbs_AdminOnly_Default(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createRegularUser(t, router, "alice", "alicepass")

	// Unauthenticated request should get 401.
	req := httptest.NewRequest("GET", "/_all_dbs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Regular user should get 401 (not a server admin).
	req = httptest.NewRequest("GET", "/_all_dbs", nil)
	req.SetBasicAuth("alice", "alicepass")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAllDbs_AdminOnly_False(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createRegularUser(t, router, "alice", "alicepass")
	createDbs(t, router, "mydb")

	// Set admin_only_all_dbs to false.
	setConfig(t, router, "chttpd", "admin_only_all_dbs", "false")

	// Regular user should now be able to access _all_dbs.
	req := httptest.NewRequest("GET", "/_all_dbs", nil)
	req.SetBasicAuth("alice", "alicepass")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var dbs []string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&dbs))
	assert.Contains(t, dbs, "mydb")
}
