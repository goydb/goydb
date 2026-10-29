package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBUpdates_NoDatabases(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	result, err := c.DBUpdates(t.Context())
	require.NoError(t, err)

	results, _ := result["results"].([]interface{})
	assert.Empty(t, results)
	assert.Equal(t, "0", result["last_seq"])
}

func TestDBUpdates_WithDatabases(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "db1")
	require.NoError(t, err)
	_, err = s.CreateDatabase(ctx, "db2")
	require.NoError(t, err)

	result, err := c.DBUpdates(ctx)
	require.NoError(t, err)

	results, _ := result["results"].([]interface{})
	assert.GreaterOrEqual(t, len(results), 2)

	// Check that all events are "updated" type
	for _, ev := range results {
		event, _ := ev.(map[string]interface{})
		require.NotNil(t, event)
		assert.Equal(t, "updated", event["type"])
		assert.NotEmpty(t, event["db_name"])
		assert.NotEmpty(t, event["seq"])
	}
	assert.NotEqual(t, "0", result["last_seq"])
}
