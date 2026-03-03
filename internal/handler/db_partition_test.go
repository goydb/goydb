//go:build !nogoja

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPartitionDB(t *testing.T) (http.Handler, func()) {
	t.Helper()
	s, router, cleanup := setupRevsDiffTest(t)

	_, err := s.CreateDatabase(t.Context(), "partdb")
	require.NoError(t, err)

	// Create documents in two partitions.
	for _, id := range []string{"sensors:temp1", "sensors:temp2", "sensors:humidity1", "users:alice", "users:bob"} {
		req := httptest.NewRequest("PUT", "/partdb/"+id, strings.NewReader(`{"type":"doc"}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth("admin", "secret")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, "creating %s: %s", id, w.Body.String())
	}
	return router, cleanup
}

func TestPartitionInfo(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/partdb/_partition/sensors", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "partdb", resp["db_name"])
	assert.Equal(t, "sensors", resp["partition"])
	assert.Equal(t, float64(3), resp["doc_count"])
}

func TestPartitionInfo_UsersPartition(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/partdb/_partition/users", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "users", resp["partition"])
	assert.Equal(t, float64(2), resp["doc_count"])
}

func TestPartitionInfo_EmptyPartition(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/partdb/_partition/nonexistent", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["doc_count"])
}

func TestPartitionAllDocs(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/partdb/_partition/sensors/_all_docs", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp AllDocsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Rows, 3)

	// Verify all rows are in the sensors partition.
	for _, row := range resp.Rows {
		assert.True(t, strings.HasPrefix(row.ID, "sensors:"), "expected sensors partition, got %s", row.ID)
	}
}

func TestPartitionAllDocs_IncludeDocs(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/partdb/_partition/users/_all_docs?include_docs=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp AllDocsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Rows, 2)
	for _, row := range resp.Rows {
		assert.NotNil(t, row.Doc)
		assert.True(t, strings.HasPrefix(row.ID, "users:"))
	}
}

func TestPartitionAllDocs_Limit(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/partdb/_partition/sensors/_all_docs?limit=1", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp AllDocsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Rows, 1)
}

func TestPartitionFind(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	body := `{"selector":{"type":"doc"}}`
	req := httptest.NewRequest("POST", "/partdb/_partition/sensors/_find", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	docs, ok := resp["docs"].([]interface{})
	require.True(t, ok)
	assert.Len(t, docs, 3)

	for _, d := range docs {
		doc := d.(map[string]interface{})
		id := doc["_id"].(string)
		assert.True(t, strings.HasPrefix(id, "sensors:"), "expected sensors: prefix, got %s", id)
	}
}

func TestPartitionFind_UsersOnly(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	body := `{"selector":{"type":"doc"}}`
	req := httptest.NewRequest("POST", "/partdb/_partition/users/_find", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	docs := resp["docs"].([]interface{})
	assert.Len(t, docs, 2)
}

func TestPartitionExplain(t *testing.T) {
	router, cleanup := setupPartitionDB(t)
	defer cleanup()

	body := `{"selector":{"type":"doc"},"limit":10}`
	req := httptest.NewRequest("POST", "/partdb/_partition/sensors/_explain", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "partdb", resp["dbname"])
	assert.Equal(t, "sensors", resp["partition"])
	assert.Equal(t, true, resp["partitioned"])
	assert.Equal(t, float64(10), resp["limit"])
}

// setupPartitionViewDB creates a database with partitioned documents and a
// design doc containing a simple view, using setupViewTest (which provides
// JavaScript engine and task controller needed for view indexing).
func setupPartitionViewDB(t *testing.T) (*mux.Router, func()) {
	t.Helper()
	s, router, cleanup := setupViewTest(t)

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
	putDesignDoc(t, router, "partdb", "myddoc", map[string]interface{}{
		"views": map[string]interface{}{
			"by_type": map[string]interface{}{
				"map":    `function(doc) { emit(doc.type, 1); }`,
				"reduce": "_count",
			},
		},
	})

	return router, cleanup
}

func TestPartitionView(t *testing.T) {
	router, cleanup := setupPartitionViewDB(t)
	defer cleanup()

	// Query the partition view for "sensors" with reduce=false.
	req := httptest.NewRequest("GET", "/partdb/_partition/sensors/_design/myddoc/_view/by_type?reduce=false", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp ViewResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Only sensors:* documents should appear.
	assert.Len(t, resp.Rows, 2, "expected 2 sensor rows, got %d", len(resp.Rows))
	for _, row := range resp.Rows {
		assert.True(t, strings.HasPrefix(row.ID, "sensors:"),
			"expected sensors: prefix, got %s", row.ID)
	}

	// Verify users:* documents are excluded.
	for _, row := range resp.Rows {
		assert.False(t, strings.HasPrefix(row.ID, "users:"),
			"unexpected users: document %s", row.ID)
	}
}

func TestPartitionView_Reduce(t *testing.T) {
	router, cleanup := setupPartitionViewDB(t)
	defer cleanup()

	// Query the partition view for "sensors" with reduce=true (default).
	req := httptest.NewRequest("GET", "/partdb/_partition/sensors/_design/myddoc/_view/by_type", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// Parse the reduce response — it returns rows with aggregated values.
	var resp struct {
		Rows []struct {
			Key   interface{} `json:"key"`
			Value interface{} `json:"value"`
		} `json:"rows"`
	}
	require.NoError(t, json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&resp))

	// With _count reduce over the sensors partition, we expect count=2
	// (sensors:temp1 and sensors:temp2 both have type "sensor").
	require.Len(t, resp.Rows, 1, "expected 1 reduced row")
	// _count returns a number; JSON decodes as float64.
	assert.Equal(t, float64(2), resp.Rows[0].Value,
		"expected count of 2 for sensors partition")
}
