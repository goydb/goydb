package handler

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSlashTest spins up a real in-process goydb HTTP server with a test
// database named "testdb". Returns the storage, client, and a cleanup
// function. Unlike setupClientTest, the router here has UseEncodedPath()
// enabled, which gorilla/mux requires to match routes against raw,
// still-escaped paths instead of decoding "%2F" back to "/" before
// matching -- required for this file's slash-in-document-ID tests.
func setupSlashTest(t *testing.T) (*storage.Storage, *client.Client, func()) {
	t.Helper()
	log := logger.NewNoLog()
	s, err := storage.Open(t.TempDir(), storage.WithLogger(log))
	require.NoError(t, err)

	r := mux.NewRouter()
	r.UseEncodedPath()
	err = Router{
		Storage:      s,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	_, err = s.CreateDatabase(context.Background(), "testdb")
	require.NoError(t, err)

	ts := httptest.NewServer(r)
	c, err := client.New(ts.URL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)

	return s, c, func() {
		ts.Close()
		_ = s.Close()
	}
}

// TestDocIDWithSlashes verifies that document IDs containing slashes work
// correctly (pkg/client percent-encodes each path segment internally).
func TestDocIDWithSlashes(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	docID := "a660cdf/8989400/64b2a743/fcc105/3aca"

	rev, err := db.Put(t.Context(), &model.Document{ID: docID, Data: map[string]interface{}{"hello": "world"}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	doc, err := db.Get(t.Context(), docID)
	require.NoError(t, err)
	assert.Equal(t, docID, doc.ID)
	assert.Equal(t, "world", doc.Data["hello"])

	headRev, err := db.Head(t.Context(), docID)
	require.NoError(t, err)
	assert.NotEmpty(t, headRev)

	newRev, err := db.Delete(t.Context(), docID, rev)
	require.NoError(t, err)
	require.NotEmpty(t, newRev)

	_, err = db.Get(t.Context(), docID)
	assert.True(t, client.IsNotFound(err))
}

// TestAllDocsWithEncodedSlashKeys verifies that _all_docs startkey/endkey
// query parameters correctly handle slash-containing keys.
func TestAllDocsWithEncodedSlashKeys(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	docID := "a660cdf/8989400/64b2a743/fcc105/3aca"
	_, err := db.Put(t.Context(), &model.Document{ID: docID, Data: map[string]interface{}{"hello": "world"}})
	require.NoError(t, err)

	resp, err := db.AllDocs(t.Context(), client.AllDocsQuery{StartKey: docID, EndKey: docID})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1, "_all_docs with slash-containing keys should return the document")
	assert.Equal(t, docID, resp.Rows[0].ID)
}

// TestCopyWithSlashIDs verifies that COPY works when both the source
// document ID and the Destination contain slashes.
func TestCopyWithSlashIDs(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	srcID := "src/doc/one"
	dstID := "dst/doc/two"

	_, err := db.Put(t.Context(), &model.Document{ID: srcID, Data: map[string]interface{}{"payload": "data"}})
	require.NoError(t, err)

	newRev, err := db.Copy(t.Context(), srcID, "", dstID, "")
	require.NoError(t, err)
	require.NotEmpty(t, newRev)

	dstDoc, err := db.Get(t.Context(), dstID)
	require.NoError(t, err)
	assert.Equal(t, dstID, dstDoc.ID)
	assert.Equal(t, "data", dstDoc.Data["payload"])

	srcDoc, err := db.Get(t.Context(), srcID)
	require.NoError(t, err)
	assert.Equal(t, srcID, srcDoc.ID)
}

// TestAttachmentCRUDWithSlashDocID verifies PUT/GET/DELETE of an attachment
// on a document whose ID contains slashes.
func TestAttachmentCRUDWithSlashDocID(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	docID := "my/slash/doc"

	rev, err := db.Put(t.Context(), &model.Document{ID: docID, Data: map[string]interface{}{"kind": "test"}})
	require.NoError(t, err)

	attRev, err := db.PutAttachment(t.Context(), docID, "readme.txt", rev, "text/plain", strings.NewReader("attachment content"))
	require.NoError(t, err)
	require.NotEmpty(t, attRev)

	att, err := db.GetAttachment(t.Context(), docID, "readme.txt")
	require.NoError(t, err)
	require.Equal(t, "text/plain", att.ContentType)
	data, err := slashReadAttachment(att)
	require.NoError(t, err)
	assert.Equal(t, "attachment content", string(data))

	delRev, err := db.DeleteAttachment(t.Context(), docID, "readme.txt", attRev)
	require.NoError(t, err)
	require.NotEmpty(t, delRev)

	_, err = db.GetAttachment(t.Context(), docID, "readme.txt")
	assert.True(t, client.IsNotFound(err))
}

// TestDesignDocWithSlashName verifies that a design document with a slash in
// its name can be stored and retrieved.
func TestDesignDocWithSlashName(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	ddocID := "_design/my/view"

	rev, err := db.Put(t.Context(), &model.Document{ID: ddocID, Data: map[string]interface{}{"views": map[string]interface{}{}}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	doc, err := db.Get(t.Context(), ddocID)
	require.NoError(t, err)
	assert.Equal(t, ddocID, doc.ID)
}

// TestLocalDocWithSlashID verifies that a local document with a slash in its
// ID can be stored and retrieved.
func TestLocalDocWithSlashID(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	localID := "_local/my/checkpoint"

	rev, err := db.Put(t.Context(), &model.Document{ID: localID, Data: map[string]interface{}{"seq": 42}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	doc, err := db.Get(t.Context(), localID)
	require.NoError(t, err)
	assert.Equal(t, localID, doc.ID)
	assert.Equal(t, float64(42), doc.Data["seq"])
}

// TestBulkGetWithSlashIDs verifies that _bulk_get works when the request
// contains document IDs with slashes.
func TestBulkGetWithSlashIDs(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	docID := "bulk/get/slash/doc"
	_, err := db.Put(t.Context(), &model.Document{ID: docID, Data: map[string]interface{}{"value": "ok"}})
	require.NoError(t, err)

	docs, err := db.BulkGet(t.Context(), []client.BulkGetRequest{{ID: docID}})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, docID, docs[0].ID)
}

// TestAllDocsPostKeysWithSlashes verifies that POST _all_docs with a
// {"keys":[...]} body correctly retrieves documents whose IDs contain
// slashes.
func TestAllDocsPostKeysWithSlashes(t *testing.T) {
	_, c, cleanup := setupSlashTest(t)
	defer cleanup()
	db := c.DB("testdb")

	docID := "a/b/c"
	_, err := db.Put(t.Context(), &model.Document{ID: docID, Data: map[string]interface{}{"hello": "world"}})
	require.NoError(t, err)

	resp, err := db.AllDocs(t.Context(), client.AllDocsQuery{Keys: []string{docID}})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1)
	assert.Equal(t, docID, resp.Rows[0].ID)
	assert.Empty(t, resp.Rows[0].Error)
}

func slashReadAttachment(r *client.AttachmentReader) ([]byte, error) {
	defer r.Close() //nolint:errcheck
	return io.ReadAll(r)
}
