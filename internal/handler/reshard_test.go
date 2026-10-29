package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReshardGet(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	result, err := c.GetReshard(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "running", result["state"])
	assert.Equal(t, float64(0), result["total"])
}

func TestReshardState_Get(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	result, err := c.GetReshardState(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "running", result["state"])
}

func TestReshardJobs_Get(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	result, err := c.ListReshardJobs(t.Context())
	require.NoError(t, err)

	jobs, _ := result["jobs"].([]interface{})
	assert.Empty(t, jobs)
	assert.Equal(t, float64(0), result["total_rows"])
}
