package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBCreate_ReturnsOkTrue(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	err := c.DB("newdb").Create(t.Context())
	require.NoError(t, err)
}

func TestDBCreate_AcceptsShardingParams(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	// CouchDB sharding/partition params should be accepted (ignored in embedded mode).
	err := c.DB("newdb").Create(t.Context(), map[string]string{"q": "8", "n": "3", "partitioned": "true"})
	require.NoError(t, err)
}

func TestDBCreate_ConflictIfExists(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	require.NoError(t, c.DB("newdb").Create(t.Context()))

	// second PUT to same db
	err := c.DB("newdb").Create(t.Context())
	assert.Equal(t, http.StatusConflict, statusCode(t, err, http.StatusOK))
}
