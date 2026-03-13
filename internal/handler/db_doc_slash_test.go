package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSlashTest creates a router with UseEncodedPath() enabled (required for
// %2F-in-path handling), a storage backend, and a test database named "testdb".
// Returns the storage, router, and a cleanup function.
func setupSlashTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-slash-*")
	require.NoError(t, err)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	r.UseEncodedPath()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	_, err = s.CreateDatabase(context.Background(), "testdb")
	require.NoError(t, err)

	return s, r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

// TestDocIDWithSlashes verifies that document IDs containing slashes
// work correctly when percent-encoded as %2F in the URL path.
func TestDocIDWithSlashes(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-slash-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s.Close()

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	r.UseEncodedPath()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	docID := "a660cdf/8989400/64b2a743/fcc105/3aca"
	encodedDocID := "a660cdf%2F8989400%2F64b2a743%2Ffcc105%2F3aca"

	// PUT a document with slashes in the ID.
	body := map[string]interface{}{"hello": "world"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/testdb/"+encodedDocID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "PUT response: %s", w.Body.String())

	var putResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &putResp)
	require.NoError(t, err)
	assert.Equal(t, docID, putResp["id"])
	assert.Equal(t, true, putResp["ok"])

	// GET the document back using the percent-encoded ID.
	req = httptest.NewRequest("GET", "/testdb/"+encodedDocID, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "GET response: %s", w.Body.String())

	var getResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &getResp)
	require.NoError(t, err)
	assert.Equal(t, docID, getResp["_id"])
	assert.Equal(t, "world", getResp["hello"])

	// HEAD should also work.
	req = httptest.NewRequest("HEAD", "/testdb/"+encodedDocID, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("ETag"))

	// DELETE the document.
	rev := putResp["rev"].(string)
	req = httptest.NewRequest("DELETE", "/testdb/"+encodedDocID+"?rev="+rev, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "DELETE response: %s", w.Body.String())

	// Confirm it's gone.
	req = httptest.NewRequest("GET", "/testdb/"+encodedDocID, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestAllDocsWithEncodedSlashKeys verifies that _all_docs startkey/endkey
// query parameters correctly handle %2F-encoded slashes.
func TestAllDocsWithEncodedSlashKeys(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-slash-alldocs-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := storage.Open(dir, storage.WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s.Close()

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	r.UseEncodedPath()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// PUT a document with slashes in the ID.
	docID := "a660cdf/8989400/64b2a743/fcc105/3aca"
	encodedDocID := "a660cdf%2F8989400%2F64b2a743%2Ffcc105%2F3aca"

	body := map[string]interface{}{"hello": "world"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/testdb/"+encodedDocID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT response: %s", w.Body.String())

	// Query _all_docs with %2F-encoded startkey/endkey.
	req = httptest.NewRequest("GET",
		`/testdb/_all_docs?startkey="a660cdf%2F8989400%2F64b2a743%2Ffcc105%2F3aca"&endkey="a660cdf%2F8989400%2F64b2a743%2Ffcc105%2F3aca"`,
		nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "_all_docs encoded response: %s", w.Body.String())

	var encodedResp struct {
		TotalRows int `json:"total_rows"`
		Rows      []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &encodedResp)
	require.NoError(t, err)
	require.Len(t, encodedResp.Rows, 1, "_all_docs with %%2F-encoded keys should return the document")
	assert.Equal(t, docID, encodedResp.Rows[0].ID)

	// Query _all_docs with literal slashes in startkey/endkey (for comparison).
	req = httptest.NewRequest("GET",
		`/testdb/_all_docs?startkey="a660cdf/8989400/64b2a743/fcc105/3aca"&endkey="a660cdf/8989400/64b2a743/fcc105/3aca"`,
		nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "_all_docs literal response: %s", w.Body.String())

	var literalResp struct {
		TotalRows int `json:"total_rows"`
		Rows      []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &literalResp)
	require.NoError(t, err)
	require.Len(t, literalResp.Rows, 1, "_all_docs with literal slash keys should return the document")
	assert.Equal(t, docID, literalResp.Rows[0].ID)
}

// TestCopyWithSlashIDs verifies that the COPY method works when both the source
// document ID and the Destination header contain slashes.
func TestCopyWithSlashIDs(t *testing.T) {
	_, r, cleanup := setupSlashTest(t)
	defer cleanup()

	srcID := "src/doc/one"
	srcEncoded := "src%2Fdoc%2Fone"
	dstID := "dst/doc/two"

	// PUT source document.
	bodyBytes, _ := json.Marshal(map[string]interface{}{"payload": "data"})
	req := httptest.NewRequest("PUT", "/testdb/"+srcEncoded, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT src: %s", w.Body.String())

	// COPY to a slash-containing destination.
	req = httptest.NewRequest("COPY", "/testdb/"+srcEncoded, nil)
	req.Header.Set("Destination", dstID)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "COPY response: %s", w.Body.String())

	var copyResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &copyResp))
	assert.Equal(t, true, copyResp["ok"])
	assert.Equal(t, dstID, copyResp["id"])

	// GET destination doc and verify _id.
	dstEncoded := "dst%2Fdoc%2Ftwo"
	req = httptest.NewRequest("GET", "/testdb/"+dstEncoded, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "GET dst: %s", w.Body.String())

	var dstDoc map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dstDoc))
	assert.Equal(t, dstID, dstDoc["_id"])
	assert.Equal(t, "data", dstDoc["payload"])

	// Source doc is still intact.
	req = httptest.NewRequest("GET", "/testdb/"+srcEncoded, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var srcDoc map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &srcDoc))
	assert.Equal(t, srcID, srcDoc["_id"])
}

// TestAttachmentCRUDWithSlashDocID verifies PUT/GET/DELETE of an attachment
// on a document whose ID contains slashes.
func TestAttachmentCRUDWithSlashDocID(t *testing.T) {
	_, r, cleanup := setupSlashTest(t)
	defer cleanup()

	docID := "my/slash/doc"
	docEncoded := "my%2Fslash%2Fdoc"

	// Create the document.
	bodyBytes, _ := json.Marshal(map[string]interface{}{"kind": "test"})
	req := httptest.NewRequest("PUT", "/testdb/"+docEncoded, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT doc: %s", w.Body.String())

	var putResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &putResp))
	rev := putResp["rev"].(string)

	// PUT attachment.
	attPath := "/testdb/" + docEncoded + "/readme.txt"
	req = httptest.NewRequest("PUT", attPath+"?rev="+rev, strings.NewReader("attachment content"))
	req.Header.Set("Content-Type", "text/plain")
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT attachment: %s", w.Body.String())

	var attPutResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &attPutResp))
	assert.Equal(t, true, attPutResp["ok"])
	assert.Equal(t, docID, attPutResp["id"])
	attRev := attPutResp["rev"].(string)

	// GET attachment.
	req = httptest.NewRequest("GET", attPath, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "GET attachment: %s", w.Body.String())
	assert.Equal(t, "attachment content", w.Body.String())
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))

	// DELETE attachment.
	req = httptest.NewRequest("DELETE", attPath+"?rev="+attRev, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "DELETE attachment: %s", w.Body.String())

	var delResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &delResp))
	assert.Equal(t, true, delResp["ok"])
	assert.Equal(t, docID, delResp["id"])

	// Confirm attachment is gone.
	req = httptest.NewRequest("GET", attPath, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestDesignDocWithSlashName verifies that a design document with a slash in
// its name can be stored and retrieved via %2F encoding.
func TestDesignDocWithSlashName(t *testing.T) {
	_, r, cleanup := setupSlashTest(t)
	defer cleanup()

	// _design/my/view — the slash after _design/ is the fixed separator;
	// the slash within the name is encoded as %2F.
	ddocID := "_design/my/view"
	ddocPath := "/testdb/_design/my%2Fview"

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"views": map[string]interface{}{},
	})
	req := httptest.NewRequest("PUT", ddocPath, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT _design: %s", w.Body.String())

	var putResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &putResp))
	assert.Equal(t, true, putResp["ok"])
	assert.Equal(t, ddocID, putResp["id"])

	// GET it back.
	req = httptest.NewRequest("GET", ddocPath, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "GET _design: %s", w.Body.String())

	var getResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &getResp))
	assert.Equal(t, ddocID, getResp["_id"])
}

// TestLocalDocWithSlashID verifies that a local document with a slash in its
// ID can be stored and retrieved via %2F encoding.
func TestLocalDocWithSlashID(t *testing.T) {
	_, r, cleanup := setupSlashTest(t)
	defer cleanup()

	localID := "_local/my/checkpoint"
	localPath := "/testdb/_local/my%2Fcheckpoint"

	bodyBytes, _ := json.Marshal(map[string]interface{}{"seq": 42})
	req := httptest.NewRequest("PUT", localPath, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT _local: %s", w.Body.String())

	var putResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &putResp))
	assert.Equal(t, true, putResp["ok"])
	assert.Equal(t, localID, putResp["id"])

	// GET it back.
	req = httptest.NewRequest("GET", localPath, nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "GET _local: %s", w.Body.String())

	var getResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &getResp))
	assert.Equal(t, localID, getResp["_id"])
	assert.Equal(t, float64(42), getResp["seq"])
}

// TestBulkGetWithSlashIDs verifies that _bulk_get works when the JSON body
// contains document IDs with slashes.
func TestBulkGetWithSlashIDs(t *testing.T) {
	_, r, cleanup := setupSlashTest(t)
	defer cleanup()

	docID := "bulk/get/slash/doc"
	docEncoded := "bulk%2Fget%2Fslash%2Fdoc"

	// PUT the document.
	bodyBytes, _ := json.Marshal(map[string]interface{}{"value": "ok"})
	req := httptest.NewRequest("PUT", "/testdb/"+docEncoded, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT: %s", w.Body.String())

	// POST _bulk_get with the slash ID in the JSON body.
	bulkBody, _ := json.Marshal(map[string]interface{}{
		"docs": []map[string]interface{}{{"id": docID}},
	})
	req = httptest.NewRequest("POST", "/testdb/_bulk_get", bytes.NewReader(bulkBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "_bulk_get: %s", w.Body.String())

	var bulkResp struct {
		Results []struct {
			ID   string                   `json:"id"`
			Docs []map[string]interface{} `json:"docs"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bulkResp))
	require.Len(t, bulkResp.Results, 1)
	assert.Equal(t, docID, bulkResp.Results[0].ID)
	require.Len(t, bulkResp.Results[0].Docs, 1)
	okDoc, hasOk := bulkResp.Results[0].Docs[0]["ok"].(map[string]interface{})
	require.True(t, hasOk, "expected ok field in result doc")
	assert.Equal(t, docID, okDoc["_id"])
}

// TestAllDocsPostKeysWithSlashes verifies that POST _all_docs with a {"keys":[...]}
// body correctly retrieves documents whose IDs contain slashes.
func TestAllDocsPostKeysWithSlashes(t *testing.T) {
	_, r, cleanup := setupSlashTest(t)
	defer cleanup()

	docID := "a/b/c"
	docEncoded := "a%2Fb%2Fc"

	// PUT the document.
	bodyBytes, _ := json.Marshal(map[string]interface{}{"hello": "world"})
	req := httptest.NewRequest("PUT", "/testdb/"+docEncoded, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "PUT: %s", w.Body.String())

	// POST _all_docs with the slash ID as a key.
	keysBody, _ := json.Marshal(map[string]interface{}{
		"keys": []string{docID},
	})
	req = httptest.NewRequest("POST", "/testdb/_all_docs", bytes.NewReader(keysBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "_all_docs POST: %s", w.Body.String())

	var allDocsResp struct {
		TotalRows int `json:"total_rows"`
		Rows      []struct {
			ID    string      `json:"id"`
			Error string      `json:"error"`
			Value interface{} `json:"value"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &allDocsResp))
	require.Len(t, allDocsResp.Rows, 1)
	assert.Equal(t, docID, allDocsResp.Rows[0].ID)
	assert.Empty(t, allDocsResp.Rows[0].Error)
}
