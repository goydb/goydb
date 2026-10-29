package handler

import (
	"net/http"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBsInfo_AdminOnly_Default(t *testing.T) {
	_, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	admin, err := client.New(baseURL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	createRegularUser(t, admin, "bob", "bobpass")

	// Unauthenticated request should get 401.
	anon, err := client.New(baseURL)
	require.NoError(t, err)
	_, err = anon.DBsInfo(t.Context(), []string{"_users"})
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, statusCode(t, err, http.StatusOK))

	// Regular user should get 401 (not a server admin).
	bob, err := client.New(baseURL, client.WithBasicAuth("bob", "bobpass"))
	require.NoError(t, err)
	_, err = bob.DBsInfo(t.Context(), []string{"_users"})
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, statusCode(t, err, http.StatusOK))
}

func TestDBsInfo_AdminOnly_False(t *testing.T) {
	s, baseURL, cleanup := setupMultiAuthTest(t)
	defer cleanup()

	admin, err := client.New(baseURL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	createRegularUser(t, admin, "bob", "bobpass")
	_, err = s.CreateDatabase(t.Context(), "mydb")
	require.NoError(t, err)

	// Set admin_only_all_dbs to false.
	setConfig(t, admin, "chttpd", "admin_only_all_dbs", "false")

	// Regular user should now be able to access _dbs_info.
	bob, err := client.New(baseURL, client.WithBasicAuth("bob", "bobpass"))
	require.NoError(t, err)
	results, err := bob.DBsInfo(t.Context(), []string{"mydb"})
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "mydb", results[0]["key"])
	assert.NotNil(t, results[0]["info"])
}

func TestDBsInfo_ExistingDatabases(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "db1")
	require.NoError(t, err)
	_, err = s.CreateDatabase(ctx, "db2")
	require.NoError(t, err)

	results, err := c.DBsInfo(ctx, []string{"db1", "db2"})
	require.NoError(t, err)
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
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "existing")
	require.NoError(t, err)

	results, err := c.DBsInfo(t.Context(), []string{"existing", "nonexistent"})
	require.NoError(t, err)
	require.Len(t, results, 2)

	// First should have info
	assert.Equal(t, "existing", results[0]["key"])
	assert.NotNil(t, results[0]["info"])

	// Second should have error
	assert.Equal(t, "nonexistent", results[1]["key"])
	assert.Equal(t, "not_found", results[1]["error"])
}

func TestDBsInfo_EmptyKeys(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	results, err := c.DBsInfo(t.Context(), []string{})
	require.NoError(t, err)
	assert.Empty(t, results)
}
