package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func putDoc(t *testing.T, router http.Handler, path string, body interface{}) (code int, result map[string]interface{}) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("PUT", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

func getAttachment(t *testing.T, router http.Handler, path string) (code int, body string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	raw, _ := io.ReadAll(w.Body)
	return w.Code, string(raw)
}

// ---------------------------------------------------------------------------
// Inline base64 attachment tests
// ---------------------------------------------------------------------------

func TestPutDoc_InlineBase64_SingleAttachment(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	content := "hello attachment"
	b64 := base64.StdEncoding.EncodeToString([]byte(content))

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"hello": "world",
		"_attachments": map[string]interface{}{
			"note.txt": map[string]interface{}{
				"content_type": "text/plain",
				"data":         b64,
			},
		},
	})

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])

	// Verify attachment is retrievable.
	attCode, attBody := getAttachment(t, router, "/testdb/doc1/note.txt")
	assert.Equal(t, http.StatusOK, attCode)
	assert.Equal(t, content, attBody)
}

func TestPutDoc_InlineBase64_MultipleAttachments(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	files := map[string]string{
		"a.txt": "file A content",
		"b.txt": "file B content",
	}

	atts := map[string]interface{}{}
	for name, content := range files {
		atts[name] = map[string]interface{}{
			"content_type": "text/plain",
			"data":         base64.StdEncoding.EncodeToString([]byte(content)),
		}
	}

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":          "doc1",
		"_attachments": atts,
	})

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])

	for name, content := range files {
		attCode, attBody := getAttachment(t, router, "/testdb/doc1/"+name)
		assert.Equal(t, http.StatusOK, attCode, "attachment %s should exist", name)
		assert.Equal(t, content, attBody, "attachment %s content mismatch", name)
	}
}

func TestPutDoc_InlineBase64_InvalidBase64(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, _ := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id": "doc1",
		"_attachments": map[string]interface{}{
			"bad.txt": map[string]interface{}{
				"content_type": "text/plain",
				"data":         "!!!not valid base64!!!",
			},
		},
	})

	assert.Equal(t, http.StatusBadRequest, code)
}

func TestPutDoc_InlineBase64_StubAttachmentsIgnored(t *testing.T) {
	// An _attachments entry with no "data" key is a stub reference and must
	// not cause an error or a spurious write.
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Step 1: create doc with a real attachment via inline base64.
	content := "original content"
	code1, result1 := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id": "doc1",
		"_attachments": map[string]interface{}{
			"note.txt": map[string]interface{}{
				"content_type": "text/plain",
				"data":         base64.StdEncoding.EncodeToString([]byte(content)),
			},
		},
	})
	require.Equal(t, http.StatusCreated, code1)
	rev := result1["rev"].(string)

	// Step 2: PUT the doc again with a stub _attachments entry and no "data" —
	// this simulates a replication client sending back the existing metadata.
	// Must not error or overwrite the attachment.
	code2, _ := putDoc(t, router, "/testdb/doc1",
		map[string]interface{}{
			"_id":  "doc1",
			"_rev": rev,
			"_attachments": map[string]interface{}{
				"note.txt": map[string]interface{}{
					"content_type": "text/plain",
					"stub":         true,
					"length":       len(content),
				},
			},
		},
	)

	assert.Equal(t, http.StatusCreated, code2)
}

func TestPutDoc_NoAttachments_StillWorks(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":  "doc1",
		"name": "plain doc",
	})

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}

// ---------------------------------------------------------------------------
// multipart/related tests
// ---------------------------------------------------------------------------

func buildMultipart(boundary string, parts []struct{ headers map[string]string; body string }) []byte {
	var buf bytes.Buffer
	for _, p := range parts {
		buf.WriteString("--" + boundary + "\r\n")
		for k, v := range p.headers {
			buf.WriteString(k + ": " + v + "\r\n")
		}
		buf.WriteString("\r\n")
		buf.WriteString(p.body)
		buf.WriteString("\r\n")
	}
	buf.WriteString("--" + boundary + "--")
	return buf.Bytes()
}

func putMultipart(t *testing.T, router http.Handler, path, boundary string, body []byte) (code int, result map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("PUT", path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "multipart/related; boundary="+boundary)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

func TestPutDoc_Multipart_SingleAttachment(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	boundary := "gc0pTestBoundary"
	body := buildMultipart(boundary, []struct {
		headers map[string]string
		body    string
	}{
		{
			headers: map[string]string{"Content-Type": "application/json"},
			body:    `{"_id":"doc2","hello":"world"}`,
		},
		{
			headers: map[string]string{
				"Content-Type":        "text/plain",
				"Content-Disposition": `attachment; filename="note.txt"`,
			},
			body: "hello multipart",
		},
	})

	code, result := putMultipart(t, router, "/testdb/doc2", boundary, body)

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc2", result["id"])

	attCode, attBody := getAttachment(t, router, "/testdb/doc2/note.txt")
	assert.Equal(t, http.StatusOK, attCode)
	assert.Equal(t, "hello multipart", attBody)
}

func TestPutDoc_Multipart_MultipleAttachments(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	boundary := "gc0pMultiBoundary"
	body := buildMultipart(boundary, []struct {
		headers map[string]string
		body    string
	}{
		{
			headers: map[string]string{"Content-Type": "application/json"},
			body:    `{"_id":"doc3"}`,
		},
		{
			headers: map[string]string{
				"Content-Type":        "text/plain",
				"Content-Disposition": `attachment; filename="alpha.txt"`,
			},
			body: "alpha content",
		},
		{
			headers: map[string]string{
				"Content-Type":        "text/plain",
				"Content-Disposition": `attachment; filename="beta.txt"`,
			},
			body: "beta content",
		},
	})

	code, result := putMultipart(t, router, "/testdb/doc3", boundary, body)

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])

	alphaCode, alphaBody := getAttachment(t, router, "/testdb/doc3/alpha.txt")
	assert.Equal(t, http.StatusOK, alphaCode)
	assert.Equal(t, "alpha content", alphaBody)

	betaCode, betaBody := getAttachment(t, router, "/testdb/doc3/beta.txt")
	assert.Equal(t, http.StatusOK, betaCode)
	assert.Equal(t, "beta content", betaBody)
}

func TestPutDoc_Multipart_DocIDFromURL(t *testing.T) {
	// _id absent from JSON body — falls back to URL path variable.
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	boundary := "gc0pURLBoundary"
	body := buildMultipart(boundary, []struct {
		headers map[string]string
		body    string
	}{
		{
			headers: map[string]string{"Content-Type": "application/json"},
			body:    `{"hello":"world"}`, // no _id
		},
		{
			headers: map[string]string{
				"Content-Type":        "text/plain",
				"Content-Disposition": `attachment; filename="file.txt"`,
			},
			body: "url-id content",
		},
	})

	code, result := putMultipart(t, router, "/testdb/docFromURL", boundary, body)

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "docFromURL", result["id"])

	attCode, attBody := getAttachment(t, router, "/testdb/docFromURL/file.txt")
	assert.Equal(t, http.StatusOK, attCode)
	assert.Equal(t, "url-id content", attBody)
}

func TestPutDoc_Multipart_InvalidJSON(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	boundary := "gc0pBadJSON"
	body := buildMultipart(boundary, []struct {
		headers map[string]string
		body    string
	}{
		{
			headers: map[string]string{"Content-Type": "application/json"},
			body:    `not valid json`,
		},
	})

	code, _ := putMultipart(t, router, "/testdb/doc1", boundary, body)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestPutDoc_Multipart_NoAttachments(t *testing.T) {
	// JSON-only multipart (no attachment parts) is valid.
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	boundary := "gc0pNoAtts"
	body := buildMultipart(boundary, []struct {
		headers map[string]string
		body    string
	}{
		{
			headers: map[string]string{"Content-Type": "application/json"},
			body:    `{"_id":"docNoAtts","x":1}`,
		},
	})

	code, result := putMultipart(t, router, "/testdb/docNoAtts", boundary, body)

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "docNoAtts", result["id"])
}

// ---------------------------------------------------------------------------
// resolveDocID helper tests
// ---------------------------------------------------------------------------

func TestPutDoc_DesignDocPrefix(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, router, "/testdb/_design/mydesign", map[string]interface{}{
		"views": map[string]interface{}{},
	})

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
	// The stored ID should carry the design doc prefix.
	assert.Contains(t, result["id"].(string), "mydesign")
}

func TestPutDoc_LocalDocPrefix(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"checkpoint": "data",
	})

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "_local/checkpoint1", result["id"])
}

// ---------------------------------------------------------------------------
// batch=ok and new_edits tests
// ---------------------------------------------------------------------------

func TestPutDoc_BatchOk(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	b, _ := json.Marshal(map[string]interface{}{"_id": "doc1", "hello": "world"})
	req := httptest.NewRequest("PUT", "/testdb/doc1?batch=ok", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
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

func TestPutDoc_NewEditsFalse(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	b, _ := json.Marshal(map[string]interface{}{
		"_id":  "doc1",
		"_rev": "1-abc",
		"data": "replicated",
	})
	req := httptest.NewRequest("PUT", "/testdb/doc1?new_edits=false", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.Equal(t, "1-abc", result["rev"])
}
