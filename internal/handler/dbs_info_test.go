package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBsInfo_AdminOnly_Default(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createRegularUser(t, router, "bob", "bobpass")

	// Unauthenticated request should get 401.
	body := strings.NewReader(`{"keys":["_users"]}`)
	req := httptest.NewRequest("POST", "/_dbs_info", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Regular user should get 401 (not a server admin).
	body = strings.NewReader(`{"keys":["_users"]}`)
	req = httptest.NewRequest("POST", "/_dbs_info", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("bob", "bobpass")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDBsInfo_AdminOnly_False(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	createRegularUser(t, router, "bob", "bobpass")
	_, err := s.CreateDatabase(t.Context(), "mydb")
	require.NoError(t, err)

	// Set admin_only_all_dbs to false.
	setConfig(t, router, "chttpd", "admin_only_all_dbs", "false")

	// Regular user should now be able to access _dbs_info.
	body := strings.NewReader(`{"keys":["mydb"]}`)
	req := httptest.NewRequest("POST", "/_dbs_info", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("bob", "bobpass")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var results []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&results))
	require.Len(t, results, 1)
	assert.Equal(t, "mydb", results[0]["key"])
	assert.NotNil(t, results[0]["info"])
}

func TestDBsInfo_ExistingDatabases(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "db1")
	require.NoError(t, err)
	_, err = s.CreateDatabase(ctx, "db2")
	require.NoError(t, err)

	body := strings.NewReader(`{"keys":["db1","db2"]}`)
	req := httptest.NewRequest("POST", "/_dbs_info", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var results []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&results))
	require.Len(t, results, 2)

	assert.Equal(t, "db1", results[0]["key"])
	info1, ok := results[0]["info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "db1", info1["db_name"])

	assert.Equal(t, "db2", results[1]["key"])
	info2, ok := results[1]["info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "db2", info2["db_name"])
}

func TestDBsInfo_MixedExistingAndMissing(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "existing")
	require.NoError(t, err)

	body := strings.NewReader(`{"keys":["existing","nonexistent"]}`)
	req := httptest.NewRequest("POST", "/_dbs_info", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var results []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&results))
	require.Len(t, results, 2)

	// First should have info
	assert.Equal(t, "existing", results[0]["key"])
	assert.NotNil(t, results[0]["info"])

	// Second should have error
	assert.Equal(t, "nonexistent", results[1]["key"])
	assert.Equal(t, "not_found", results[1]["error"])
}

func TestDBsInfo_EmptyKeys(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	body := strings.NewReader(`{"keys":[]}`)
	req := httptest.NewRequest("POST", "/_dbs_info", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var results []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&results))
	assert.Empty(t, results)
}
