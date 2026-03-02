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

// setupVDUTest creates a test environment with validate engines registered.
func setupVDUTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-vdu-test-*")
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

// vduPutDoc PUTs a document and returns the status code and decoded JSON response.
func vduPutDoc(t *testing.T, router http.Handler, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("PUT", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result := map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

// vduPostDoc POSTs a document and returns the status code and decoded JSON response.
func vduPostDoc(t *testing.T, router http.Handler, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result := map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

// vduDeleteDoc DELETEs a document and returns the status code and decoded JSON response.
func vduDeleteDoc(t *testing.T, router http.Handler, path string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("DELETE", path, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result := map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

// vduBulkDocs POSTs to _bulk_docs and returns the status code and decoded JSON response.
func vduBulkDocs(t *testing.T, router http.Handler, dbName string, body interface{}) (int, []map[string]interface{}) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/"+dbName+"/_bulk_docs", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var result []map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

// createDesignDocWithVDU creates a design document with a validate_doc_update function.
func createDesignDocWithVDU(t *testing.T, router http.Handler, dbName, ddocName, vduFn string) string {
	t.Helper()
	code, result := vduPutDoc(t, router, "/"+dbName+"/_design/"+ddocName, map[string]interface{}{
		"language":            "javascript",
		"validate_doc_update": vduFn,
	})
	require.Equal(t, http.StatusCreated, code, "failed to create design doc: %v", result)
	return result["rev"].(string)
}

// --- Tests ---

func TestVDU_NoVDUFunctions_WritesSucceed(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	code, result := vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id": "doc1",
		"foo": "bar",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}

func TestVDU_CustomVDU_RejectsMissingField(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create a VDU that requires a "title" field.
	createDesignDocWithVDU(t, router, "testdb", "myvalidator",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (!newDoc._deleted && !newDoc.title) {
				throw({forbidden: "document must have a title"});
			}
		}`)

	// Try to create a doc without title -> should fail.
	code, result := vduPutDoc(t, router, "/testdb/nodoc", map[string]interface{}{
		"_id": "nodoc",
		"foo": "bar",
	})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, result["reason"], "document must have a title")

	// Create a doc with title -> should succeed.
	code, result = vduPutDoc(t, router, "/testdb/hasdoc", map[string]interface{}{
		"_id":   "hasdoc",
		"title": "Hello",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}

func TestVDU_ThrowsForbidden_Returns403(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithVDU(t, router, "testdb", "forbidder",
		`function(newDoc, oldDoc, userCtx, secObj) {
			throw({forbidden: "nope"});
		}`)

	code, result := vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id": "doc1",
	})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, result["reason"], "nope")
}

func TestVDU_ThrowsUnauthorized_Returns401(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithVDU(t, router, "testdb", "authchecker",
		`function(newDoc, oldDoc, userCtx, secObj) {
			throw({unauthorized: "login required"});
		}`)

	code, result := vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id": "doc1",
	})
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, result["reason"], "login required")
}

func TestVDU_MultipleVDUs_AllMustPass(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// VDU 1: requires "title" (skip design docs)
	createDesignDocWithVDU(t, router, "testdb", "v1",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (newDoc._id.indexOf('_design/') === 0) return;
			if (!newDoc._deleted && !newDoc.title) {
				throw({forbidden: "must have title"});
			}
		}`)

	// VDU 2: requires "author" (skip design docs)
	createDesignDocWithVDU(t, router, "testdb", "v2",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (newDoc._id.indexOf('_design/') === 0) return;
			if (!newDoc._deleted && !newDoc.author) {
				throw({forbidden: "must have author"});
			}
		}`)

	// Missing both -> should fail (one of them).
	code, _ := vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id": "doc1",
	})
	assert.Equal(t, http.StatusForbidden, code)

	// Has title but no author -> should still fail.
	code, _ = vduPutDoc(t, router, "/testdb/doc2", map[string]interface{}{
		"_id":   "doc2",
		"title": "Hello",
	})
	assert.Equal(t, http.StatusForbidden, code)

	// Has both -> should succeed.
	code, result := vduPutDoc(t, router, "/testdb/doc3", map[string]interface{}{
		"_id":    "doc3",
		"title":  "Hello",
		"author": "Alice",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}

func TestVDU_DeleteValidated(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// VDU that forbids deletes.
	createDesignDocWithVDU(t, router, "testdb", "nodelete",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (newDoc._deleted) {
				throw({forbidden: "deletes not allowed"});
			}
		}`)

	// Create a doc first (should pass since not a delete).
	code, result := vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":  "doc1",
		"data": "hello",
	})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// Delete -> should fail.
	code, result = vduDeleteDoc(t, router, "/testdb/doc1?rev="+rev)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, result["reason"], "deletes not allowed")
}

func TestVDU_ReplicationSkipsVDU(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// VDU that rejects everything.
	createDesignDocWithVDU(t, router, "testdb", "rejectall",
		`function(newDoc, oldDoc, userCtx, secObj) {
			throw({forbidden: "all writes rejected"});
		}`)

	// new_edits=false bypasses VDU.
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
}

func TestVDU_POSTEndpointValidated(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithVDU(t, router, "testdb", "check",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (!newDoc.required_field) {
				throw({forbidden: "missing required_field"});
			}
		}`)

	// POST without required field -> rejected.
	code, result := vduPostDoc(t, router, "/testdb", map[string]interface{}{
		"other": "value",
	})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, result["reason"], "missing required_field")

	// POST with required field -> accepted.
	code, result = vduPostDoc(t, router, "/testdb", map[string]interface{}{
		"required_field": "present",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}

func TestVDU_BulkDocsPerDocErrors(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// VDU that requires "valid" field.
	createDesignDocWithVDU(t, router, "testdb", "bulkcheck",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (!newDoc._deleted && !newDoc.valid) {
				throw({forbidden: "doc must be valid"});
			}
		}`)

	code, results := vduBulkDocs(t, router, "testdb", map[string]interface{}{
		"docs": []map[string]interface{}{
			{"_id": "good1", "valid": true},
			{"_id": "bad1"},
			{"_id": "good2", "valid": true},
		},
	})
	assert.Equal(t, http.StatusOK, code)
	require.Len(t, results, 3)

	// good1 should succeed.
	assert.Equal(t, true, results[0]["ok"])
	assert.Equal(t, "good1", results[0]["id"])

	// bad1 should fail.
	assert.Equal(t, "forbidden", results[1]["error"])
	assert.Contains(t, results[1]["reason"], "doc must be valid")

	// good2 should succeed.
	assert.Equal(t, true, results[2]["ok"])
	assert.Equal(t, "good2", results[2]["id"])
}

func TestVDU_BulkDocsReplicationSkipsVDU(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	createDesignDocWithVDU(t, router, "testdb", "rejectall",
		`function(newDoc, oldDoc, userCtx, secObj) {
			throw({forbidden: "all writes rejected"});
		}`)

	newEdits := false
	code, results := vduBulkDocs(t, router, "testdb", map[string]interface{}{
		"new_edits": newEdits,
		"docs": []map[string]interface{}{
			{"_id": "doc1", "_rev": "1-abc", "data": "replicated"},
		},
	})
	assert.Equal(t, http.StatusOK, code)
	require.Len(t, results, 1)
	assert.Equal(t, true, results[0]["ok"])
}

func TestVDU_DesignDocUpdatesValidated(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// VDU that rejects everything except design docs and deletes.
	createDesignDocWithVDU(t, router, "testdb", "strictvdu",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (newDoc._id.indexOf('_design/') === 0) return;
			throw({forbidden: "regular docs not allowed"});
		}`)

	// Creating another design doc should work (the existing VDU allows design docs).
	code, result := vduPutDoc(t, router, "/testdb/_design/another", map[string]interface{}{
		"language": "javascript",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])

	// Creating a regular doc should fail.
	code, result = vduPutDoc(t, router, "/testdb/regular", map[string]interface{}{
		"_id": "regular",
	})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, result["reason"], "regular docs not allowed")
}

func TestVDU_UsersDBValidation(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	// Ensure system databases are created (seeds _design/_auth with VDU).
	err := s.EnsureSystemDatabases(t.Context())
	require.NoError(t, err)

	// PUT a user doc missing type: "user" -> should get 403.
	code, result := vduPutDoc(t, router, "/_users/org.couchdb.user:testuser", map[string]interface{}{
		"_id":  "org.couchdb.user:testuser",
		"name": "testuser",
	})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, result["reason"], "doc.type must be user")

	// PUT a well-formed user doc -> should succeed.
	code, result = vduPutDoc(t, router, "/_users/org.couchdb.user:testuser", map[string]interface{}{
		"_id":      "org.couchdb.user:testuser",
		"name":     "testuser",
		"type":     "user",
		"roles":    []string{},
		"password": "secret123",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}

func TestVDU_VDUReceivesOldDoc(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// VDU that prevents changing the "owner" field once set.
	createDesignDocWithVDU(t, router, "testdb", "ownerlock",
		`function(newDoc, oldDoc, userCtx, secObj) {
			if (oldDoc && oldDoc.owner && newDoc.owner !== oldDoc.owner) {
				throw({forbidden: "cannot change owner"});
			}
		}`)

	// Create doc with owner.
	code, result := vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"owner": "alice",
	})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// Try to change owner -> should fail.
	code, result = vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"_rev":  rev,
		"owner": "bob",
	})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, result["reason"], "cannot change owner")

	// Keep same owner -> should succeed.
	code, result = vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":   "doc1",
		"_rev":  rev,
		"owner": "alice",
		"data":  "updated",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}

func TestVDU_CompilationErrorSkipped(t *testing.T) {
	s, router, cleanup := setupVDUTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create a design doc with broken JS — should not block writes.
	// We store it via new_edits=false to bypass any VDU on the design doc itself.
	b, _ := json.Marshal(map[string]interface{}{
		"_id":  "_design/broken",
		"_rev": "1-abc",
		"language":            "javascript",
		"validate_doc_update": "this is not valid javascript{{{",
	})
	req := httptest.NewRequest("PUT", "/testdb/_design/broken?new_edits=false", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Now try to write a regular doc — should succeed (broken VDU is skipped).
	code, result := vduPutDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id":  "doc1",
		"data": "hello",
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Equal(t, true, result["ok"])
}
