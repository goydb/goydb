//go:build !nogoja

package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupUpdateTest creates a test environment with update engines registered.
func setupUpdateTest(t *testing.T) (*storage.Storage, *client.Client, func()) {
	t.Helper()
	log := logger.NewNoLog()
	c, s, cleanup := setupClientTest(t,
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
		storage.WithFilterEngine("", gojaview.NewFilterServer),
		storage.WithFilterEngine("javascript", gojaview.NewFilterServer),
		storage.WithReducerEngine("", gojaview.NewReducerBuilder(log)),
		storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(log)),
		storage.WithValidateEngine("", gojaview.NewValidateServerBuilder(log)),
		storage.WithValidateEngine("javascript", gojaview.NewValidateServerBuilder(log)),
		storage.WithUpdateEngine("", gojaview.NewUpdateServer),
		storage.WithUpdateEngine("javascript", gojaview.NewUpdateServer),
	)
	return s, c, cleanup
}

// createDesignDocWithUpdate creates a design doc with update functions.
func createDesignDocWithUpdate(t *testing.T, c *client.Client, dbName, ddocName string, updates map[string]string) string {
	t.Helper()
	doc := map[string]interface{}{
		"language": "javascript",
		"updates":  updates,
	}
	code, result := vduPutDoc(t, c, "/"+dbName+"/_design/"+ddocName, doc)
	require.Equal(t, http.StatusCreated, code, "failed to create design doc: %v", result)
	return result["rev"].(string)
}

// updatePost sends a POST to the update function endpoint without docid.
func updatePost(t *testing.T, c *client.Client, path string, body string) *http.Response {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodPost, path, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	return resp
}

// updatePut sends a PUT to the update function endpoint with docid.
func updatePut(t *testing.T, c *client.Client, path string, body string) *http.Response {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodPut, path, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	return resp
}

// readBody reads and closes resp's body, returning its content as a string.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func TestUpdate_POST_NullDoc_StringResponse(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"hello": `function(doc, req) { return [null, "Hello World"]; }`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/hello", "")
	assert.Equal(t, http.StatusOK, w.StatusCode)
	assert.Contains(t, w.Header.Get("Content-Type"), "text/html")
	assert.Equal(t, "Hello World", readBody(t, w))
}

func TestUpdate_POST_CreatesDoc(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"create": `function(doc, req) {
			var newDoc = {_id: "created-doc", type: "test", data: req.body};
			return [newDoc, "Created!"];
		}`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/create", "some data")
	assert.Equal(t, http.StatusCreated, w.StatusCode)
	assert.Equal(t, "created-doc", w.Header.Get("X-Couch-Id"))
	assert.NotEmpty(t, w.Header.Get("X-Couch-Update-NewRev"))
	assert.Equal(t, "Created!", readBody(t, w))
}

func TestUpdate_PUT_ModifiesExistingDoc(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create the target doc first
	code, result := vduPutDoc(t, c, "/testdb/target", map[string]interface{}{
		"_id":   "target",
		"count": 0,
	})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"increment": `function(doc, req) {
			doc.count = (doc.count || 0) + 1;
			return [doc, "Updated count to " + doc.count];
		}`,
	})

	w := updatePut(t, c, "/testdb/_design/myddoc/_update/increment/target", "")
	assert.Equal(t, http.StatusCreated, w.StatusCode)
	assert.Equal(t, "target", w.Header.Get("X-Couch-Id"))
	newRev := w.Header.Get("X-Couch-Update-NewRev")
	assert.NotEmpty(t, newRev)
	assert.NotEqual(t, rev, newRev)
	assert.Contains(t, readBody(t, w), "Updated count to 1")

	// Verify the doc was actually updated
	getCode, getResult := vduGetDoc(t, c, "/testdb/target")
	assert.Equal(t, http.StatusOK, getCode)
	assert.Equal(t, float64(1), getResult["count"])
}

func TestUpdate_PUT_DocNotFound_ReceivesNull(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"upsert": `function(doc, req) {
			if (!doc) {
				return [{_id: "newdoc", created: true}, "Created new"];
			}
			return [doc, "Existing"];
		}`,
	})

	w := updatePut(t, c, "/testdb/_design/myddoc/_update/upsert/nonexistent", "")
	assert.Equal(t, http.StatusCreated, w.StatusCode)
	assert.Equal(t, "Created new", readBody(t, w))

	// Verify the doc was created
	getCode, getResult := vduGetDoc(t, c, "/testdb/newdoc")
	assert.Equal(t, http.StatusOK, getCode)
	assert.Equal(t, true, getResult["created"])
}

func TestUpdate_ResponseString_ContentType(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"text": `function(doc, req) { return [null, "plain text"]; }`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/text", "")
	assert.Equal(t, http.StatusOK, w.StatusCode)
	assert.Contains(t, w.Header.Get("Content-Type"), "text/html;charset=utf-8")
	readBody(t, w)
}

func TestUpdate_ResponseObject_CustomHeaders(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"custom": `function(doc, req) {
			return [null, {
				code: 200,
				headers: {"X-Custom": "foobar", "Content-Type": "text/plain"},
				body: "custom body"
			}];
		}`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/custom", "")
	assert.Equal(t, http.StatusOK, w.StatusCode)
	assert.Equal(t, "foobar", w.Header.Get("X-Custom"))
	assert.Equal(t, "text/plain", w.Header.Get("Content-Type"))
	assert.Equal(t, "custom body", readBody(t, w))
}

func TestUpdate_ResponseJSON(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"jsonresp": `function(doc, req) {
			return [null, {json: {ok: true, message: "done"}}];
		}`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/jsonresp", "")
	assert.Equal(t, http.StatusOK, w.StatusCode)
	assert.Contains(t, w.Header.Get("Content-Type"), "application/json")

	defer w.Body.Close()
	var result map[string]interface{}
	err = json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "done", result["message"])
}

func TestUpdate_VDU_RejectsSavedDoc(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// First create a VDU that rejects docs without "title"
	createDesignDocWithVDU(t, c, "testdb", "myvdu",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (newDoc._id.indexOf('_design/') === 0) return;
			if (!newDoc._deleted && !newDoc.title) {
				throw({forbidden: "document must have a title"});
			}
		}`)

	// Then create an update function that creates docs without title
	createDesignDocWithUpdate(t, c, "testdb", "myupdate", map[string]string{
		"notitle": `function(doc, req) {
			return [{_id: "notitle-doc", data: "no title here"}, "Created"];
		}`,
	})

	w := updatePost(t, c, "/testdb/_design/myupdate/_update/notitle", "")
	assert.Equal(t, http.StatusForbidden, w.StatusCode)
	readBody(t, w)
}

func TestUpdate_ReqObject_Populated(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"echo": `function(doc, req) {
			return [null, {json: {
				method: req.method,
				body: req.body,
				query: req.query,
				id: req.id,
				hasUserCtx: !!req.userCtx,
				hasSecObj: !!req.secObj
			}}];
		}`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/echo?foo=bar", "request body")
	assert.Equal(t, http.StatusOK, w.StatusCode)

	defer w.Body.Close()
	var result map[string]interface{}
	err = json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "POST", result["method"])
	assert.Equal(t, "request body", result["body"])
	assert.Equal(t, true, result["hasUserCtx"])
	assert.Equal(t, true, result["hasSecObj"])

	query, ok := result["query"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "bar", query["foo"])
}

func TestUpdate_DesignDocNotFound(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	w := updatePost(t, c, "/testdb/_design/nonexistent/_update/whatever", "")
	assert.Equal(t, http.StatusNotFound, w.StatusCode)
	readBody(t, w)
}

func TestUpdate_FunctionNameNotFound(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"exists": `function(doc, req) { return [null, "ok"]; }`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/doesnotexist", "")
	assert.Equal(t, http.StatusNotFound, w.StatusCode)
	readBody(t, w)
}

func TestUpdate_JSError_Returns500(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"broken": `function(doc, req) { throw new Error("oops"); }`,
	})

	w := updatePost(t, c, "/testdb/_design/myddoc/_update/broken", "")
	assert.Equal(t, http.StatusInternalServerError, w.StatusCode)
	readBody(t, w)
}

func TestUpdate_ConflictOnSave(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create a doc
	code, _ := vduPutDoc(t, c, "/testdb/conflictdoc", map[string]interface{}{
		"_id":  "conflictdoc",
		"data": "initial",
	})
	require.Equal(t, http.StatusCreated, code)

	// Update function that returns a doc with a stale/wrong rev
	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"stalerev": `function(doc, req) {
			doc._rev = "1-stalerevision";
			doc.data = "changed";
			return [doc, "Updated"];
		}`,
	})

	w := updatePut(t, c, "/testdb/_design/myddoc/_update/stalerev/conflictdoc", "")
	assert.Equal(t, http.StatusConflict, w.StatusCode)
	readBody(t, w)
}

func TestUpdate_PUT_WithDocID_CreatesNewDoc(t *testing.T) {
	s, c, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, c, "testdb", "myddoc", map[string]string{
		"init": `function(doc, req) {
			if (!doc) {
				return [{_id: req.id, initialized: true}, "Initialized"];
			}
			return [doc, "Already exists"];
		}`,
	})

	w := updatePut(t, c, "/testdb/_design/myddoc/_update/init/brandnew", "")
	assert.Equal(t, http.StatusCreated, w.StatusCode)
	assert.Equal(t, "brandnew", w.Header.Get("X-Couch-Id"))
	assert.Equal(t, "Initialized", readBody(t, w))

	// Verify the doc exists
	getCode, getResult := vduGetDoc(t, c, "/testdb/brandnew")
	assert.Equal(t, http.StatusOK, getCode)
	assert.Equal(t, true, getResult["initialized"])
}
