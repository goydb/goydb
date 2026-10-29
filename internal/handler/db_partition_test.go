//go:build !nogoja

package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPartitionDB(t *testing.T) (*client.Client, func()) {
	t.Helper()
	s, c, cleanup := setupRevsDiffTest(t)

	_, err := s.CreateDatabase(t.Context(), "partdb")
	require.NoError(t, err)

	// Create documents in two partitions.
	for _, id := range []string{"sensors:temp1", "sensors:temp2", "sensors:humidity1", "users:alice", "users:bob"} {
		_, err := c.DB("partdb").Put(t.Context(), &model.Document{ID: id, Data: map[string]interface{}{"type": "doc"}})
		require.NoError(t, err, "creating %s", id)
	}
	return c, cleanup
}

func TestPartitionInfo(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("sensors").Info(t.Context())
	require.NoError(t, err)

	assert.Equal(t, "partdb", resp["db_name"])
	assert.Equal(t, "sensors", resp["partition"])
	assert.Equal(t, float64(3), resp["doc_count"])
}

func TestPartitionInfo_UsersPartition(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("users").Info(t.Context())
	require.NoError(t, err)

	assert.Equal(t, "users", resp["partition"])
	assert.Equal(t, float64(2), resp["doc_count"])
}

func TestPartitionInfo_EmptyPartition(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("nonexistent").Info(t.Context())
	require.NoError(t, err)

	assert.Equal(t, float64(0), resp["doc_count"])
}

func TestPartitionAllDocs(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("sensors").AllDocs(t.Context(), client.AllDocsQuery{})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 3)

	// Verify all rows are in the sensors partition.
	for _, row := range resp.Rows {
		assert.True(t, strings.HasPrefix(row.ID, "sensors:"), "expected sensors partition, got %s", row.ID)
	}
}

func TestPartitionAllDocs_IncludeDocs(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("users").AllDocs(t.Context(), client.AllDocsQuery{IncludeDocs: true})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 2)
	for _, row := range resp.Rows {
		assert.NotNil(t, row.Doc)
		assert.True(t, strings.HasPrefix(row.ID, "users:"))
	}
}

func TestPartitionAllDocs_Limit(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("sensors").AllDocs(t.Context(), client.AllDocsQuery{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 1)
}

func TestPartitionFind(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("sensors").Find(t.Context(), client.FindQuery{
		Selector: map[string]interface{}{"type": "doc"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Docs, 3)

	for _, doc := range resp.Docs {
		id := doc["_id"].(string)
		assert.True(t, strings.HasPrefix(id, "sensors:"), "expected sensors: prefix, got %s", id)
	}
}

func TestPartitionFind_UsersOnly(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("users").Find(t.Context(), client.FindQuery{
		Selector: map[string]interface{}{"type": "doc"},
	})
	require.NoError(t, err)
	assert.Len(t, resp.Docs, 2)
}

func TestPartitionExplain(t *testing.T) {
	c, cleanup := setupPartitionDB(t)
	defer cleanup()

	resp, err := c.DB("partdb").Partition("sensors").Explain(t.Context(), client.FindQuery{
		Selector: map[string]interface{}{"type": "doc"},
		Limit:    10,
	})
	require.NoError(t, err)
	assert.Equal(t, "partdb", resp["dbname"])
	assert.Equal(t, "sensors", resp["partition"])
	assert.Equal(t, true, resp["partitioned"])
	assert.Equal(t, float64(10), resp["limit"])
}

// setupPartitionViewDB creates a database with partitioned documents and a
// design doc containing a simple view, using setupViewTest (which provides
// JavaScript engine and task controller needed for view indexing).
func setupPartitionViewDB(t *testing.T) (*client.Client, func()) {
	t.Helper()
	s, c, cleanup := setupViewTest(t)

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "partdb")
	require.NoError(t, err)

	// Create documents in two partitions with a "type" field.
	docs := []struct {
		id   string
		data map[string]interface{}
	}{
		{"sensors:temp1", map[string]interface{}{"type": "sensor", "reading": 22.5}},
		{"sensors:temp2", map[string]interface{}{"type": "sensor", "reading": 23.1}},
		{"users:alice", map[string]interface{}{"type": "user", "name": "alice"}},
		{"users:bob", map[string]interface{}{"type": "user", "name": "bob"}},
	}
	for _, d := range docs {
		_, err := db.PutDocument(ctx, &model.Document{ID: d.id, Data: d.data})
		require.NoError(t, err)
	}

	// Create a design doc with a view that emits by type.
	putDesignDoc(t, c, "partdb", "myddoc", map[string]interface{}{
		"views": map[string]interface{}{
			"by_type": map[string]interface{}{
				"map":    `function(doc) { emit(doc.type, 1); }`,
				"reduce": "_count",
			},
		},
	})

	return c, cleanup
}

func TestPartitionView(t *testing.T) {
	c, cleanup := setupPartitionViewDB(t)
	defer cleanup()

	// Query the partition view for "sensors" with reduce=false.
	resp, err := c.DB("partdb").Raw(t.Context(), http.MethodGet, "/_partition/sensors/_design/myddoc/_view/by_type?reduce=false", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result client.RowsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	// Only sensors:* documents should appear.
	assert.Len(t, result.Rows, 2, "expected 2 sensor rows, got %d", len(result.Rows))
	for _, row := range result.Rows {
		assert.True(t, strings.HasPrefix(row.ID, "sensors:"),
			"expected sensors: prefix, got %s", row.ID)
	}

	// Verify users:* documents are excluded.
	for _, row := range result.Rows {
		assert.False(t, strings.HasPrefix(row.ID, "users:"),
			"unexpected users: document %s", row.ID)
	}
}

func TestPartitionView_Reduce(t *testing.T) {
	c, cleanup := setupPartitionViewDB(t)
	defer cleanup()

	// Query the partition view for "sensors" with reduce=true (default).
	resp, err := c.DB("partdb").Raw(t.Context(), http.MethodGet, "/_partition/sensors/_design/myddoc/_view/by_type", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse the reduce response — it returns rows with aggregated values.
	var result struct {
		Rows []struct {
			Key   interface{} `json:"key"`
			Value interface{} `json:"value"`
		} `json:"rows"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	// With _count reduce over the sensors partition, we expect count=2
	// (sensors:temp1 and sensors:temp2 both have type "sensor").
	require.Len(t, result.Rows, 1, "expected 1 reduced row")
	// _count returns a number; JSON decodes as float64.
	assert.Equal(t, float64(2), result.Rows[0].Value,
		"expected count of 2 for sensors partition")
}
