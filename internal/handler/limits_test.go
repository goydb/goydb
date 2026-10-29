package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// setConfig sets a config value via the PUT /_config/{section}/{key} API.
func setConfig(t *testing.T, c *client.Client, section, key, value string) {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf("%q", value))
	resp, err := c.Raw(t.Context(), http.MethodPut, "/_config/"+section+"/"+key, body, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "setConfig %s/%s=%s failed: %s", section, key, value, string(raw))
}

// createDB creates a database via PUT /{db} and returns the status code.
func createDB(t *testing.T, c *client.Client, name string) int {
	t.Helper()
	resp, err := c.DB(name).Raw(t.Context(), http.MethodPut, "", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	return resp.StatusCode
}

// postDoc creates a document via POST /{db} and returns the status code and response.
func postDoc(t *testing.T, c *client.Client, db, body string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := c.DB(db).Raw(t.Context(), http.MethodPost, "", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// putDocRaw puts a raw JSON string as a document via PUT /{db}/{docid}.
func putDocRaw(t *testing.T, c *client.Client, path, body string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodPut, path, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// bulkDocs posts a _bulk_docs request and returns the status code and raw body.
func bulkDocs(t *testing.T, c *client.Client, db, body string) (int, string) {
	t.Helper()
	resp, err := c.DB(db).Raw(t.Context(), http.MethodPost, "/_bulk_docs", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// ---------------------------------------------------------------------------
// max_dbs
// ---------------------------------------------------------------------------

func TestLimits_MaxDbs_BlocksExcess(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	setConfig(t, c, "couchdb", "max_dbs", "2")

	// The storage may already contain system DBs (_users, _replicator).
	// Create databases until we hit the limit.
	assert.Equal(t, http.StatusCreated, createDB(t, c, "db1"))
	assert.Equal(t, http.StatusCreated, createDB(t, c, "db2"))

	// Third database should be blocked.
	code := createDB(t, c, "db3")
	assert.Equal(t, http.StatusPreconditionFailed, code)
}

func TestLimits_MaxDbs_ZeroMeansUnlimited(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	setConfig(t, c, "couchdb", "max_dbs", "0")

	for i := 0; i < 5; i++ {
		code := createDB(t, c, fmt.Sprintf("db%d", i))
		assert.Equal(t, http.StatusCreated, code)
	}
}

// ---------------------------------------------------------------------------
// max_document_size
// ---------------------------------------------------------------------------

func TestLimits_MaxDocumentSize_BlocksLargeDoc(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_document_size", "100")

	// Small doc should succeed.
	code, _ := putDocRaw(t, c, "/testdb/small", `{"_id":"small","x":"y"}`)
	assert.Equal(t, http.StatusCreated, code)

	// Large doc should be rejected.
	largeBody := `{"_id":"big","data":"` + strings.Repeat("x", 200) + `"}`
	code, result := putDocRaw(t, c, "/testdb/big", largeBody)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Equal(t, "document_too_large", result["reason"])
}

func TestLimits_MaxDocumentSize_PostEndpoint(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_document_size", "50")

	// Small doc via POST.
	code, _ := postDoc(t, c, "testdb", `{"x":"y"}`)
	assert.Equal(t, http.StatusCreated, code)

	// Large doc via POST.
	largeBody := `{"data":"` + strings.Repeat("x", 100) + `"}`
	code, result := postDoc(t, c, "testdb", largeBody)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Equal(t, "document_too_large", result["reason"])
}

func TestLimits_MaxDocumentSize_BulkDocs(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_document_size", "50")

	// One small doc and one large doc in the batch.
	body := `{"docs":[{"_id":"s","x":"y"},{"_id":"big","data":"` + strings.Repeat("x", 100) + `"}]}`
	code, _ := bulkDocs(t, c, "testdb", body)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
}

// ---------------------------------------------------------------------------
// max_docs_per_db
// ---------------------------------------------------------------------------

func TestLimits_MaxDocsPerDB_BlocksExcess(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_docs_per_db", "2")

	code, _ := putDocRaw(t, c, "/testdb/doc1", `{"_id":"doc1"}`)
	assert.Equal(t, http.StatusCreated, code)

	code, _ = putDocRaw(t, c, "/testdb/doc2", `{"_id":"doc2"}`)
	assert.Equal(t, http.StatusCreated, code)

	// Third doc should be blocked.
	code, result := putDocRaw(t, c, "/testdb/doc3", `{"_id":"doc3"}`)
	assert.Equal(t, http.StatusPreconditionFailed, code)
	assert.Contains(t, result["reason"], "maximum number of documents")
}

func TestLimits_MaxDocsPerDB_UpdatesDoNotCount(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_docs_per_db", "2")

	code, result := putDocRaw(t, c, "/testdb/doc1", `{"_id":"doc1"}`)
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	code, _ = putDocRaw(t, c, "/testdb/doc2", `{"_id":"doc2"}`)
	require.Equal(t, http.StatusCreated, code)

	// Updating doc1 (has _rev) should succeed even though we're at limit.
	code, _ = putDocRaw(t, c, "/testdb/doc1", fmt.Sprintf(`{"_id":"doc1","_rev":"%s","updated":true}`, rev))
	assert.Equal(t, http.StatusCreated, code)
}

func TestLimits_MaxDocsPerDB_BulkDocsBatchCounted(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_docs_per_db", "2")

	// Batch of 3 new docs should be blocked.
	body := `{"docs":[{"_id":"a"},{"_id":"b"},{"_id":"c"}]}`
	code, _ := bulkDocs(t, c, "testdb", body)
	assert.Equal(t, http.StatusPreconditionFailed, code)

	// Batch of 2 should succeed.
	body = `{"docs":[{"_id":"d"},{"_id":"e"}]}`
	code, _ = bulkDocs(t, c, "testdb", body)
	assert.Equal(t, http.StatusOK, code)
}

func TestLimits_MaxDocsPerDB_SkippedForNewEditsFalse(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_docs_per_db", "1")

	// Create one doc to fill the limit.
	code, _ := putDocRaw(t, c, "/testdb/doc1", `{"_id":"doc1"}`)
	require.Equal(t, http.StatusCreated, code)

	// Replication-mode doc (new_edits=false) should bypass the limit.
	code, _ = putDocRaw(t, c, "/testdb/doc2?new_edits=false", `{"_id":"doc2","_rev":"1-abc"}`)
	assert.Equal(t, http.StatusCreated, code)
}

func TestLimits_MaxDocsPerDB_PostAlwaysCounts(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_docs_per_db", "1")

	code, _ := postDoc(t, c, "testdb", `{"x":"y"}`)
	assert.Equal(t, http.StatusCreated, code)

	code, _ = postDoc(t, c, "testdb", `{"x":"z"}`)
	assert.Equal(t, http.StatusPreconditionFailed, code)
}

// ---------------------------------------------------------------------------
// max_attachment_size
// ---------------------------------------------------------------------------

func TestLimits_MaxAttachmentSize_BlocksLargeAttachment(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_attachment_size", "10")

	// Create the doc first.
	code, result := putDocRaw(t, c, "/testdb/doc1", `{"_id":"doc1"}`)
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// Small attachment should succeed.
	code, result = putAttachmentWithRev(t, c, "/testdb/doc1/small.txt", "text/plain", "hi", rev)
	assert.Equal(t, http.StatusCreated, code)
	rev = result["rev"].(string)

	// Large attachment should be rejected.
	code, result = putAttachmentWithRev(t, c, "/testdb/doc1/big.txt", "text/plain", strings.Repeat("x", 50), rev)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Equal(t, "attachment_too_large", result["reason"])
}

func TestLimits_MaxAttachmentSize_InlineBase64(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	setConfig(t, c, "couchdb", "max_attachment_size", "5")

	// Inline attachment with data > 5 bytes (base64 of "hello world" = "aGVsbG8gd29ybGQ=", decoded = 11 bytes).
	body := `{"_id":"doc1","_attachments":{"file.txt":{"content_type":"text/plain","data":"aGVsbG8gd29ybGQ="}}}`
	code, result := putDocRaw(t, c, "/testdb/doc1", body)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Equal(t, "attachment_too_large", result["reason"])
}

// ---------------------------------------------------------------------------
// max_db_size
// ---------------------------------------------------------------------------

func TestLimits_MaxDBSize_BlocksWritesWhenExceeded(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Write some documents to grow the DB file.
	for i := 0; i < 5; i++ {
		code, _ := putDocRaw(t, c, fmt.Sprintf("/testdb/doc%d", i),
			fmt.Sprintf(`{"_id":"doc%d","data":"%s"}`, i, strings.Repeat("x", 500)))
		require.Equal(t, http.StatusCreated, code)
	}

	// Set max_db_size to 1 byte — effectively blocking all further writes.
	setConfig(t, c, "couchdb", "max_db_size", "1")

	code, result := putDocRaw(t, c, "/testdb/blocked", `{"_id":"blocked"}`)
	assert.Equal(t, http.StatusPreconditionFailed, code)
	assert.Contains(t, result["reason"], "maximum allowed database size")
}

// ---------------------------------------------------------------------------
// Runtime config change
// ---------------------------------------------------------------------------

func TestLimits_RuntimeConfigChange(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	// No limit initially — create 3 DBs.
	for i := 0; i < 3; i++ {
		code := createDB(t, c, fmt.Sprintf("rdb%d", i))
		assert.Equal(t, http.StatusCreated, code)
	}

	// Set limit to 3 via API.
	setConfig(t, c, "couchdb", "max_dbs", "3")

	// Now a 4th should be blocked.
	code := createDB(t, c, "rdb3")
	assert.Equal(t, http.StatusPreconditionFailed, code)

	// Raise the limit.
	setConfig(t, c, "couchdb", "max_dbs", "10")

	// Now creation should work again.
	code = createDB(t, c, "rdb3")
	assert.Equal(t, http.StatusCreated, code)
}
