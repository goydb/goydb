package handler

import (
	"encoding/json"
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

func putAttachment(t *testing.T, c *client.Client, path, contentType, body string) (code int, result map[string]interface{}) {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodPut, path, strings.NewReader(body), map[string]string{"Content-Type": contentType})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	result = map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func putAttachmentWithRev(t *testing.T, c *client.Client, path, contentType, body, rev string) (code int, result map[string]interface{}) {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodPut, path+"?rev="+rev, strings.NewReader(body), map[string]string{"Content-Type": contentType})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	result = map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func headAttachment(t *testing.T, c *client.Client, path string) *http.Response {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodHead, path, nil, nil)
	require.NoError(t, err)
	return resp
}

func deleteAttachment(t *testing.T, c *client.Client, path, rev string) (code int, result map[string]interface{}) {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodDelete, path+"?rev="+rev, nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	result = map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// createDocAndAttachment is a small fixture: creates a doc then PUTs an attachment,
// returning the new rev after the attachment write.
func createDocAndAttachment(t *testing.T, c *client.Client, db, docID, attName, content string) (docRev string) {
	t.Helper()

	// 1. create the doc
	code, result := putDoc(t, c, "/"+db+"/"+docID, map[string]interface{}{"_id": docID})
	require.Equal(t, http.StatusCreated, code)
	docRev = result["rev"].(string)

	// 2. put the attachment
	code, result = putAttachmentWithRev(t, c, "/"+db+"/"+docID+"/"+attName, "text/plain", content, docRev)
	require.Equal(t, http.StatusCreated, code, "putAttachment: %v", result)
	return result["rev"].(string)
}

// ---------------------------------------------------------------------------
// PUT attachment
// ---------------------------------------------------------------------------

func TestPutAttachment_FirstTime_ReturnsFullResponse(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// create doc first
	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// PUT attachment with rev
	code, result = putAttachmentWithRev(t, c, "/testdb/doc1/file.txt", "text/plain", "hello", rev)
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

func TestPutAttachment_ConflictOnStaleRev(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	_ = result["rev"].(string)

	// Use a clearly wrong rev
	code, _ = putAttachmentWithRev(t, c, "/testdb/doc1/file.txt", "text/plain", "hello", "1-wrongrev")
	assert.Equal(t, http.StatusConflict, code)
}

func TestPutAttachment_IfMatchHeader(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	resp, err := c.Raw(t.Context(), http.MethodPut, "/testdb/doc1/file.txt", strings.NewReader("hello"), map[string]string{
		"Content-Type": "text/plain",
		"If-Match":     `"` + rev + `"`,
	})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestPutAttachment_NotFoundDoc(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, _ := putAttachment(t, c, "/testdb/nonexistent/file.txt", "text/plain", "hello")
	assert.Equal(t, http.StatusNotFound, code)
}

// ---------------------------------------------------------------------------
// GET attachment — ETag and Content-Length
// ---------------------------------------------------------------------------

func TestGetAttachment_Headers(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	content := "hello world"
	_ = createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", content)

	resp, err := c.Raw(t.Context(), http.MethodGet, "/testdb/doc1/file.txt", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
	assert.NotEmpty(t, resp.Header.Get("ETag"))
	assert.Equal(t, "11", resp.Header.Get("Content-Length")) // len("hello world")
	assert.Equal(t, content, string(bodyBytes))
}

func TestGetAttachment_RevParam(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "hello")

	code, body := getAttachment(t, c, "/testdb/doc1/file.txt?rev="+rev)

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "hello", body)
}

func TestGetAttachment_RangeRequest(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "hello world")

	resp, err := c.Raw(t.Context(), http.MethodGet, "/testdb/doc1/file.txt", nil, map[string]string{"Range": "bytes=0-4"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusPartialContent, resp.StatusCode)
	assert.Equal(t, "hello", string(bodyBytes))
	assert.Contains(t, resp.Header.Get("Content-Range"), "bytes 0-4/11")
}

func TestGetAttachment_ETagFormat(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "data")

	resp, err := c.Raw(t.Context(), http.MethodGet, "/testdb/doc1/file.txt", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	etag := resp.Header.Get("ETag")
	assert.True(t, strings.HasPrefix(etag, `"md5-`), "ETag should start with \"md5-\", got %q", etag)
	assert.True(t, strings.HasSuffix(etag, `"`), "ETag should end with \", got %q", etag)
}

// ---------------------------------------------------------------------------
// HEAD attachment
// ---------------------------------------------------------------------------

func TestHeadAttachment_ReturnsHeadersNoBody(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "hello world")

	resp := headAttachment(t, c, "/testdb/doc1/file.txt")
	defer resp.Body.Close() //nolint:errcheck
	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("ETag"))
	assert.Equal(t, "11", resp.Header.Get("Content-Length"))
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
	assert.Empty(t, string(bodyBytes))
}

func TestHeadAttachment_NotFound(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create the doc but no attachment
	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	_ = result

	resp := headAttachment(t, c, "/testdb/doc1/missing.txt")
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHeadAttachment_ETagMatchesGet(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "payload")

	headResp := headAttachment(t, c, "/testdb/doc1/file.txt")
	defer headResp.Body.Close() //nolint:errcheck

	getResp, err := c.Raw(t.Context(), http.MethodGet, "/testdb/doc1/file.txt", nil, nil)
	require.NoError(t, err)
	defer getResp.Body.Close() //nolint:errcheck

	assert.Equal(t, headResp.Header.Get("ETag"), getResp.Header.Get("ETag"))
	assert.Equal(t, headResp.Header.Get("Content-Length"), getResp.Header.Get("Content-Length"))
}

// ---------------------------------------------------------------------------
// DELETE attachment
// ---------------------------------------------------------------------------

func TestDeleteAttachment_ReturnsFullResponse(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "hello")

	code, result := deleteAttachment(t, c, "/testdb/doc1/file.txt", rev)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

func TestDeleteAttachment_ConflictOnStaleRev(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "hello")

	code, _ := deleteAttachment(t, c, "/testdb/doc1/file.txt", "1-wrongrev")
	assert.Equal(t, http.StatusConflict, code)
}

func TestDeleteAttachment_NotFoundAttachment(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	code, _ = deleteAttachment(t, c, "/testdb/doc1/missing.txt", rev)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestDeleteAttachment_RemovedFromGet(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "hello")

	code, _ := deleteAttachment(t, c, "/testdb/doc1/file.txt", rev)
	require.Equal(t, http.StatusOK, code)

	// attachment should be gone
	attCode, _ := getAttachment(t, c, "/testdb/doc1/file.txt")
	assert.Equal(t, http.StatusNotFound, attCode)
}

// ---------------------------------------------------------------------------
// batch=ok support
// ---------------------------------------------------------------------------

func TestPutAttachment_BatchOk(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, c, "/testdb/doc1", map[string]interface{}{"_id": "doc1"})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	resp, err := c.Raw(t.Context(), http.MethodPut, "/testdb/doc1/file.txt?rev="+rev+"&batch=ok", strings.NewReader("hello"), map[string]string{"Content-Type": "text/plain"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	var attResult map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&attResult)
	assert.Equal(t, true, attResult["ok"])
	assert.Equal(t, "doc1", attResult["id"])
	assert.NotEmpty(t, attResult["rev"])
}

func TestDeleteAttachment_BatchOk(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDocAndAttachment(t, c, "testdb", "doc1", "file.txt", "hello")

	resp, err := c.Raw(t.Context(), http.MethodDelete, "/testdb/doc1/file.txt?rev="+rev+"&batch=ok", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

// ---------------------------------------------------------------------------
// Design-doc attachment routes
// ---------------------------------------------------------------------------

func TestDesignDocAttachment_PutGetHead(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// create design doc
	code, result := putDoc(t, c, "/testdb/_design/myview", map[string]interface{}{
		"views": map[string]interface{}{},
	})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// PUT attachment
	code, result = putAttachmentWithRev(t, c, "/testdb/_design/myview/logo.png", "image/png", "pngdata", rev)
	require.Equal(t, http.StatusCreated, code, "%v", result)
	assert.Equal(t, true, result["ok"])

	// GET attachment
	attCode, attBody := getAttachment(t, c, "/testdb/_design/myview/logo.png")
	assert.Equal(t, http.StatusOK, attCode)
	assert.Equal(t, "pngdata", attBody)

	// HEAD attachment
	hResp := headAttachment(t, c, "/testdb/_design/myview/logo.png")
	defer hResp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, hResp.StatusCode)
	assert.NotEmpty(t, hResp.Header.Get("ETag"))
}
