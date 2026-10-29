package handler

import (
	"net/http"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplain_Basic(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	result, err := c.DB("testdb").Explain(t.Context(), client.FindQuery{
		Selector: map[string]interface{}{"name": "Alice"},
	})
	require.NoError(t, err)

	assert.Equal(t, "testdb", result["dbname"])
	assert.NotNil(t, result["index"])
	assert.NotNil(t, result["selector"])
	assert.NotNil(t, result["opts"])

	idx := result["index"].(map[string]interface{})
	assert.Equal(t, "_all_docs", idx["name"])
	assert.Equal(t, "special", idx["type"])
}

func TestExplain_WithOptions(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	result, err := c.DB("testdb").Explain(t.Context(), client.FindQuery{
		Selector: map[string]interface{}{"age": map[string]interface{}{"$gt": 21}},
		Limit:    10,
		Skip:     5,
		Fields:   []string{"name", "age"},
	})
	require.NoError(t, err)

	assert.Equal(t, float64(10), result["limit"])
	assert.Equal(t, float64(5), result["skip"])

	fields := result["fields"].([]interface{})
	assert.Contains(t, fields, "name")
	assert.Contains(t, fields, "age")
}

func TestExplain_DBNotFound(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := c.DB("nonexistent").Explain(t.Context(), client.FindQuery{
		Selector: map[string]interface{}{"name": "test"},
	})
	assert.Equal(t, http.StatusNotFound, statusCode(t, err, http.StatusOK))
}
