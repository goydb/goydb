package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPurge_Basic(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	result, err := c.DB("testdb").Purge(t.Context(), map[string][]string{
		"doc1": {"1-abc"},
		"doc2": {"1-def", "2-ghi"},
	})
	require.NoError(t, err)

	purged, _ := result["purged"].(map[string]interface{})
	require.Len(t, purged, 2)
	doc1, _ := purged["doc1"].([]interface{})
	assert.Equal(t, []interface{}{"1-abc"}, doc1)
	doc2, _ := purged["doc2"].([]interface{})
	assert.Equal(t, []interface{}{"1-def", "2-ghi"}, doc2)
}

func TestPurgedInfosLimit_Get(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	limit, err := c.DB("testdb").GetPurgedInfosLimit(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1000, limit)
}

func TestPurgedInfosLimit_Put(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	err = c.DB("testdb").SetPurgedInfosLimit(t.Context(), 500)
	require.NoError(t, err)
}
