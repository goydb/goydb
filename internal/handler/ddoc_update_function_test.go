//go:build !nogoja

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupUpdateTest creates a test environment with update engines registered.
func setupUpdateTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-update-test-*")
	require.NoError(t, err)

	log := logger.NewNoLog()
	s, err := storage.Open(dir,
		storage.WithLogger(log),
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
	require.NoError(t, err)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	return s, r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

// createDesignDocWithUpdate creates a design doc with update functions.
func createDesignDocWithUpdate(t *testing.T, router http.Handler, dbName, ddocName string, updates map[string]string) string {
	t.Helper()
	doc := map[string]interface{}{
		"language": "javascript",
		"updates":  updates,
	}
	code, result := vduPutDoc(t, router, "/"+dbName+"/_design/"+ddocName, doc)
	require.Equal(t, http.StatusCreated, code, "failed to create design doc: %v", result)
	return result["rev"].(string)
}

// updatePost sends a POST to the update function endpoint without docid.
func updatePost(t *testing.T, router http.Handler, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// updatePut sends a PUT to the update function endpoint with docid.
func updatePut(t *testing.T, router http.Handler, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestUpdate_POST_NullDoc_StringResponse(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"hello": `function(doc, req) { return [null, "Hello World"]; }`,
	})

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/hello", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Hello World", w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestUpdate_POST_CreatesDoc(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"create": `function(doc, req) {
			var newDoc = {_id: "created-doc", type: "test", data: req.body};
			return [newDoc, "Created!"];
		}`,
	})

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/create", "some data")
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "Created!", w.Body.String())
	assert.Equal(t, "created-doc", w.Header().Get("X-Couch-Id"))
	assert.NotEmpty(t, w.Header().Get("X-Couch-Update-NewRev"))
}

func TestUpdate_PUT_ModifiesExistingDoc(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create the target doc first
	code, result := vduPutDoc(t, router, "/testdb/target", map[string]interface{}{
		"_id":   "target",
		"count": 0,
	})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"increment": `function(doc, req) {
			doc.count = (doc.count || 0) + 1;
			return [doc, "Updated count to " + doc.count];
		}`,
	})

	w := updatePut(t, router, "/testdb/_design/myddoc/_update/increment/target", "")
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "Updated count to 1")
	assert.Equal(t, "target", w.Header().Get("X-Couch-Id"))
	newRev := w.Header().Get("X-Couch-Update-NewRev")
	assert.NotEmpty(t, newRev)
	assert.NotEqual(t, rev, newRev)

	// Verify the doc was actually updated
	getCode, getResult := vduGetDoc(t, router, "/testdb/target")
	assert.Equal(t, http.StatusOK, getCode)
	assert.Equal(t, float64(1), getResult["count"])
}

func TestUpdate_PUT_DocNotFound_ReceivesNull(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"upsert": `function(doc, req) {
			if (!doc) {
				return [{_id: "newdoc", created: true}, "Created new"];
			}
			return [doc, "Existing"];
		}`,
	})

	w := updatePut(t, router, "/testdb/_design/myddoc/_update/upsert/nonexistent", "")
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "Created new", w.Body.String())

	// Verify the doc was created
	getCode, getResult := vduGetDoc(t, router, "/testdb/newdoc")
	assert.Equal(t, http.StatusOK, getCode)
	assert.Equal(t, true, getResult["created"])
}

func TestUpdate_ResponseString_ContentType(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"text": `function(doc, req) { return [null, "plain text"]; }`,
	})

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/text", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html;charset=utf-8")
}

func TestUpdate_ResponseObject_CustomHeaders(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"custom": `function(doc, req) {
			return [null, {
				code: 200,
				headers: {"X-Custom": "foobar", "Content-Type": "text/plain"},
				body: "custom body"
			}];
		}`,
	})

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/custom", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "foobar", w.Header().Get("X-Custom"))
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	assert.Equal(t, "custom body", w.Body.String())
}

func TestUpdate_ResponseJSON(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"jsonresp": `function(doc, req) {
			return [null, {json: {ok: true, message: "done"}}];
		}`,
	})

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/jsonresp", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var result map[string]interface{}
	err = json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "done", result["message"])
}

func TestUpdate_VDU_RejectsSavedDoc(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// First create a VDU that rejects docs without "title"
	createDesignDocWithVDU(t, router, "testdb", "myvdu",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (newDoc._id.indexOf('_design/') === 0) return;
			if (!newDoc._deleted && !newDoc.title) {
				throw({forbidden: "document must have a title"});
			}
		}`)

	// Then create an update function that creates docs without title
	createDesignDocWithUpdate(t, router, "testdb", "myupdate", map[string]string{
		"notitle": `function(doc, req) {
			return [{_id: "notitle-doc", data: "no title here"}, "Created"];
		}`,
	})

	w := updatePost(t, router, "/testdb/_design/myupdate/_update/notitle", "")
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestUpdate_ReqObject_Populated(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
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

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/echo?foo=bar", "request body")
	assert.Equal(t, http.StatusOK, w.Code)

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
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	w := updatePost(t, router, "/testdb/_design/nonexistent/_update/whatever", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdate_FunctionNameNotFound(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"exists": `function(doc, req) { return [null, "ok"]; }`,
	})

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/doesnotexist", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdate_JSError_Returns500(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"broken": `function(doc, req) { throw new Error("oops"); }`,
	})

	w := updatePost(t, router, "/testdb/_design/myddoc/_update/broken", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdate_ConflictOnSave(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create a doc
	code, _ := vduPutDoc(t, router, "/testdb/conflictdoc", map[string]interface{}{
		"_id":  "conflictdoc",
		"data": "initial",
	})
	require.Equal(t, http.StatusCreated, code)

	// Update function that returns a doc with a stale/wrong rev
	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"stalerev": `function(doc, req) {
			doc._rev = "1-stalerevision";
			doc.data = "changed";
			return [doc, "Updated"];
		}`,
	})

	w := updatePut(t, router, "/testdb/_design/myddoc/_update/stalerev/conflictdoc", "")
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestUpdate_PUT_WithDocID_CreatesNewDoc(t *testing.T) {
	s, router, cleanup := setupUpdateTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithUpdate(t, router, "testdb", "myddoc", map[string]string{
		"init": `function(doc, req) {
			if (!doc) {
				return [{_id: req.id, initialized: true}, "Initialized"];
			}
			return [doc, "Already exists"];
		}`,
	})

	w := updatePut(t, router, "/testdb/_design/myddoc/_update/init/brandnew", "")
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "Initialized", w.Body.String())
	assert.Equal(t, "brandnew", w.Header().Get("X-Couch-Id"))

	// Verify the doc exists
	getCode, getResult := vduGetDoc(t, router, "/testdb/brandnew")
	assert.Equal(t, http.StatusOK, getCode)
	assert.Equal(t, true, getResult["initialized"])
}
