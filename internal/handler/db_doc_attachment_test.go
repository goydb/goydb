package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func putAttachment(t *testing.T, router http.Handler, path, contentType, body string) (code int, result map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("PUT", path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

func putAttachmentWithRev(t *testing.T, router http.Handler, path, contentType, body, rev string) (code int, result map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("PUT", path+"?rev="+rev, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

func headAttachment(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("HEAD", path, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func deleteAttachment(t *testing.T, router http.Handler, path, rev string) (code int, result map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("DELETE", path+"?rev="+rev, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

// createDocAndAttachment is a small fixture: creates a doc then PUTs an attachment,
// returning the new rev after the attachment write.
func createDocAndAttachment(t *testing.T, router http.Handler, db, docID, attName, content string) (docRev string) {
	t.Helper()

	// 1. create the doc
	code, result := putDoc(t, router, "/"+db+"/"+docID, map[string]interface{}{"_id": docID})
	require.Equal(t, http.StatusCreated, code)
	docRev = result["rev"].(string)

	// 2. put the attachment
	code, result = putAttachmentWithRev(t, router, "/"+db+"/"+docID+"/"+attName, "text/plain", content, docRev)
	require.Equal(t, http.StatusCreated, code, "putAttachment: %v", result)
	return result["rev"].(string)
}

// ---------------------------------------------------------------------------
// PUT attachment
// ---------------------------------------------------------------------------

func TestPutAttachment_FirstTime_ReturnsFullResponse(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// create doc first
	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// PUT attachment with rev
	code, result = putAttachmentWithRev(t, router, "/testdb/doc1/file.txt", "text/plain", "hello", rev)
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

func TestPutAttachment_ConflictOnStaleRev(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	_ = result["rev"].(string)

	// Use a clearly wrong rev
	code, _ = putAttachmentWithRev(t, router, "/testdb/doc1/file.txt", "text/plain", "hello", "1-wrongrev")
	assert.Equal(t, http.StatusConflict, code)
}

func TestPutAttachment_IfMatchHeader(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	req := httptest.NewRequest("PUT", "/testdb/doc1/file.txt", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("If-Match", `"`+rev+`"`)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestPutAttachment_NotFoundDoc(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, _ := putAttachment(t, router, "/testdb/nonexistent/file.txt", "text/plain", "hello")
	assert.Equal(t, http.StatusNotFound, code)
}

// ---------------------------------------------------------------------------
// GET attachment — ETag and Content-Length
// ---------------------------------------------------------------------------

func TestGetAttachment_Headers(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	content := "hello world"
	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", content)

	req := httptest.NewRequest("GET", "/testdb/doc1/file.txt", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	assert.NotEmpty(t, w.Header().Get("ETag"))
	assert.Equal(t, "11", w.Header().Get("Content-Length")) // len("hello world")
	assert.Equal(t, content, w.Body.String())
}

func TestGetAttachment_RevParam(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello")

	req := httptest.NewRequest("GET", "/testdb/doc1/file.txt?rev="+rev, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "hello", w.Body.String())
}

func TestGetAttachment_RangeRequest(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello world")

	req := httptest.NewRequest("GET", "/testdb/doc1/file.txt", nil)
	req.Header.Set("Range", "bytes=0-4")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusPartialContent, w.Code)
	assert.Equal(t, "hello", w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Range"), "bytes 0-4/11")
}

func TestGetAttachment_ETagFormat(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "data")

	req := httptest.NewRequest("GET", "/testdb/doc1/file.txt", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	etag := w.Header().Get("ETag")
	assert.True(t, strings.HasPrefix(etag, `"md5-`), "ETag should start with \"md5-\", got %q", etag)
	assert.True(t, strings.HasSuffix(etag, `"`), "ETag should end with \", got %q", etag)
}

// ---------------------------------------------------------------------------
// HEAD attachment
// ---------------------------------------------------------------------------

func TestHeadAttachment_ReturnsHeadersNoBody(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello world")

	w := headAttachment(t, router, "/testdb/doc1/file.txt")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("ETag"))
	assert.Equal(t, "11", w.Header().Get("Content-Length"))
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	assert.Empty(t, w.Body.String())
}

func TestHeadAttachment_NotFound(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create the doc but no attachment
	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	_ = result

	w := headAttachment(t, router, "/testdb/doc1/missing.txt")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHeadAttachment_ETagMatchesGet(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "payload")

	headW := headAttachment(t, router, "/testdb/doc1/file.txt")

	getReq := httptest.NewRequest("GET", "/testdb/doc1/file.txt", nil)
	getReq.SetBasicAuth("admin", "secret")
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	assert.Equal(t, headW.Header().Get("ETag"), getW.Header().Get("ETag"))
	assert.Equal(t, headW.Header().Get("Content-Length"), getW.Header().Get("Content-Length"))
}

// ---------------------------------------------------------------------------
// DELETE attachment
// ---------------------------------------------------------------------------

func TestDeleteAttachment_ReturnsFullResponse(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello")

	code, result := deleteAttachment(t, router, "/testdb/doc1/file.txt", rev)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

func TestDeleteAttachment_ConflictOnStaleRev(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello")

	code, _ := deleteAttachment(t, router, "/testdb/doc1/file.txt", "1-wrongrev")
	assert.Equal(t, http.StatusConflict, code)
}

func TestDeleteAttachment_NotFoundAttachment(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	code, _ = deleteAttachment(t, router, "/testdb/doc1/missing.txt", rev)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestDeleteAttachment_RemovedFromGet(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello")

	code, _ := deleteAttachment(t, router, "/testdb/doc1/file.txt", rev)
	require.Equal(t, http.StatusOK, code)

	// attachment should be gone
	attCode, _ := getAttachment(t, router, "/testdb/doc1/file.txt")
	assert.Equal(t, http.StatusNotFound, attCode)
}

// ---------------------------------------------------------------------------
// Design-doc attachment routes
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// batch=ok support
// ---------------------------------------------------------------------------

func TestPutAttachment_BatchOk(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	req := httptest.NewRequest("PUT", "/testdb/doc1/file.txt?rev="+rev+"&batch=ok", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	var attResult map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&attResult)
	assert.Equal(t, true, attResult["ok"])
	assert.Equal(t, "doc1", attResult["id"])
	assert.NotEmpty(t, attResult["rev"])
}

func TestDeleteAttachment_BatchOk(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello")

	req := httptest.NewRequest("DELETE", "/testdb/doc1/file.txt?rev="+rev+"&batch=ok", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

// ---------------------------------------------------------------------------
// Design-doc attachment routes
// ---------------------------------------------------------------------------

func TestDesignDocAttachment_PutGetHead(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// create design doc
	code, result := putDoc(t, router, "/testdb/_design/myview", map[string]interface{}{
		"views": map[string]interface{}{},
	})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// PUT attachment
	code, result = putAttachmentWithRev(t, router, "/testdb/_design/myview/logo.png", "image/png", "pngdata", rev)
	require.Equal(t, http.StatusCreated, code, "%v", result)
	assert.Equal(t, true, result["ok"])

	// GET attachment
	attCode, attBody := getAttachment(t, router, "/testdb/_design/myview/logo.png")
	assert.Equal(t, http.StatusOK, attCode)
	assert.Equal(t, "pngdata", attBody)

	// HEAD attachment
	hw := headAttachment(t, router, "/testdb/_design/myview/logo.png")
	assert.Equal(t, http.StatusOK, hw.Code)
	assert.NotEmpty(t, hw.Header().Get("ETag"))
}
