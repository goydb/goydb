package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShards_Basic(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	result, err := c.DB("testdb").Shards(t.Context(), "")
	require.NoError(t, err)

	shards, _ := result["shards"].(map[string]interface{})
	require.NotNil(t, shards)
	nodes, ok := shards["00000000-ffffffff"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"nonode@nohost"}, nodes)
}

func TestShardsDoc(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	result, err := c.DB("testdb").Shards(t.Context(), "mydoc")
	require.NoError(t, err)

	assert.Equal(t, "00000000-ffffffff", result["range"])
	nodes, ok := result["nodes"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"nonode@nohost"}, nodes)
}

func TestSyncShards(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	err = c.DB("testdb").SyncShards(t.Context())
	require.NoError(t, err)
}
