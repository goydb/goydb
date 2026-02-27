package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getDoc performs a GET request and returns the status code and decoded body.
func getDoc(t *testing.T, router http.Handler, path string) (code int, result map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

// deleteDoc performs a DELETE request and returns the status code and decoded body.
func deleteDoc(t *testing.T, router http.Handler, path string) (code int, result map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("DELETE", path, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	result = map[string]interface{}{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	return w.Code, result
}

func TestLocalDoc_GetNotFound(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	code, result := getDoc(t, router, "/testdb/_local/nonexistent")
	assert.Equal(t, http.StatusNotFound, code, "GET non-existent _local doc should 404")
	assert.Equal(t, "not_found", result["error"])
}

func TestLocalDoc_PutCreate(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create a _local doc (no _rev needed for first write).
	code, result := putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"last_seq": "5",
	})
	assert.Equal(t, http.StatusCreated, code, "PUT new _local doc should return 201")
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "_local/checkpoint1", result["id"])
	rev, _ := result["rev"].(string)
	assert.Equal(t, "0-1", rev, "first _local revision should be 0-1")
}

func TestLocalDoc_PutThenGet(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create
	code, result := putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"last_seq": "5",
	})
	require.Equal(t, http.StatusCreated, code)

	_ = result

	// GET must include _id and _rev
	code, result = getDoc(t, router, "/testdb/_local/checkpoint1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "_local/checkpoint1", result["_id"], "GET should return _id")
	assert.Equal(t, "0-1", result["_rev"], "GET should return _rev")
	assert.Equal(t, "5", result["last_seq"])
}

func TestLocalDoc_UpdateWithRev(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create
	code, result := putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"last_seq": "5",
	})
	require.Equal(t, http.StatusCreated, code)
	rev1 := result["rev"].(string)

	// Update with correct _rev
	code, result = putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"_rev":     rev1,
		"last_seq": "10",
	})
	assert.Equal(t, http.StatusCreated, code, "PUT with correct _rev should succeed")
	assert.Equal(t, "0-2", result["rev"], "second revision should be 0-2")

	// Verify the updated content via GET
	code, result = getDoc(t, router, "/testdb/_local/checkpoint1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "0-2", result["_rev"])
	assert.Equal(t, "10", result["last_seq"])
}

func TestLocalDoc_UpdateWithoutRevConflict(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create
	code, _ := putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"last_seq": "5",
	})
	require.Equal(t, http.StatusCreated, code)

	// Update WITHOUT _rev → should 409 because doc already exists.
	code, result := putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"last_seq": "10",
	})
	assert.Equal(t, http.StatusConflict, code, "PUT without _rev on existing doc should 409")
	assert.Equal(t, "conflict", result["error"])
}

func TestLocalDoc_UpdateWithStaleRevConflict(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create
	code, result := putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"last_seq": "5",
	})
	require.Equal(t, http.StatusCreated, code)
	rev1 := result["rev"].(string)

	// Update to rev 0-2
	code, _ = putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"_rev":     rev1,
		"last_seq": "10",
	})
	require.Equal(t, http.StatusCreated, code)

	// Try update with stale rev (0-1 instead of 0-2) → should 409.
	code, result = putDoc(t, router, "/testdb/_local/checkpoint1", map[string]interface{}{
		"_rev":     rev1,
		"last_seq": "15",
	})
	assert.Equal(t, http.StatusConflict, code, "PUT with stale _rev should 409")
	assert.Equal(t, "conflict", result["error"])
}

// TestLocalDoc_PouchDBCheckpointWorkflow simulates the full PouchDB checkpoint
// lifecycle: GET (404) → PUT (create) → GET (read back) → PUT (update using
// _rev from GET) → repeat. This is the pattern that was failing in the browser.
func TestLocalDoc_PouchDBCheckpointWorkflow(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	checkpointID := "iitrWYvDOwGyOUWqKATcCQ=="

	// 1) GET _local/checkpoint → 404 (first time, doesn't exist).
	code, _ := getDoc(t, router, "/testdb/_local/"+checkpointID)
	require.Equal(t, http.StatusNotFound, code, "step 1: should be 404")

	// 2) PUT _local/checkpoint to create (no _rev).
	code, result := putDoc(t, router, "/testdb/_local/"+checkpointID, map[string]interface{}{
		"_id":      "_local/" + checkpointID,
		"last_seq": "3",
		"session":  "sess-001",
	})
	require.Equal(t, http.StatusCreated, code, "step 2: create should succeed")
	rev1 := result["rev"].(string)
	assert.Equal(t, "0-1", rev1)

	// 3) GET _local/checkpoint → 200, must include _rev.
	code, result = getDoc(t, router, "/testdb/_local/"+checkpointID)
	require.Equal(t, http.StatusOK, code, "step 3: should be 200")
	assert.Equal(t, rev1, result["_rev"], "step 3: GET must return current _rev")
	assert.Equal(t, "_local/"+checkpointID, result["_id"])

	// 4) PUT _local/checkpoint to update, using _rev from GET.
	code, result = putDoc(t, router, "/testdb/_local/"+checkpointID, map[string]interface{}{
		"_id":      "_local/" + checkpointID,
		"_rev":     result["_rev"],
		"last_seq": "8",
		"session":  "sess-001",
	})
	require.Equal(t, http.StatusCreated, code, "step 4: update should succeed")
	rev2 := result["rev"].(string)
	assert.Equal(t, "0-2", rev2)

	// 5) GET again, verify updated content and _rev.
	code, result = getDoc(t, router, "/testdb/_local/"+checkpointID)
	require.Equal(t, http.StatusOK, code, "step 5: should be 200")
	assert.Equal(t, rev2, result["_rev"], "step 5: GET must return updated _rev")
	assert.Equal(t, "8", result["last_seq"])

	// 6) Another update cycle using _rev from step 5.
	code, result = putDoc(t, router, "/testdb/_local/"+checkpointID, map[string]interface{}{
		"_id":      "_local/" + checkpointID,
		"_rev":     result["_rev"],
		"last_seq": "15",
		"session":  "sess-002",
	})
	require.Equal(t, http.StatusCreated, code, "step 6: second update should succeed")
	assert.Equal(t, "0-3", result["rev"])
}

func TestLocalDoc_Delete(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// Create
	code, result := putDoc(t, router, "/testdb/_local/todelete", map[string]interface{}{
		"data": "hello",
	})
	require.Equal(t, http.StatusCreated, code)
	rev := result["rev"].(string)

	// Delete with correct rev
	code, result = deleteDoc(t, router, "/testdb/_local/todelete?rev="+rev)
	assert.Equal(t, http.StatusOK, code, "DELETE with correct rev should succeed")
	assert.Equal(t, true, result["ok"])

	// GET after delete → should be 404
	code, _ = getDoc(t, router, "/testdb/_local/todelete")
	assert.Equal(t, http.StatusNotFound, code, "GET after DELETE should 404")
}

// TestLocalDoc_PutBodyIDMismatch verifies that when the PUT body contains
// _id without the _local/ prefix, the document is still stored correctly
// and accessible via GET.
func TestLocalDoc_PutBodyIDWithoutPrefix(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// PUT with body _id that omits _local/ prefix — URL determines the prefix.
	body, _ := json.Marshal(map[string]interface{}{
		"_id":  "bare_id_no_prefix",
		"data": "test",
	})
	req := httptest.NewRequest("PUT", "/testdb/_local/bare_id_no_prefix", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)

	// The body _id takes precedence in resolveDocID, so the doc might be
	// stored as "bare_id_no_prefix" instead of "_local/bare_id_no_prefix".
	// Record what happened for diagnosis.
	t.Logf("PUT status: %d, id: %v, rev: %v", w.Code, result["id"], result["rev"])

	if w.Code == http.StatusCreated {
		// Try to GET via the _local/ route.
		code, getResult := getDoc(t, router, "/testdb/_local/bare_id_no_prefix")
		t.Logf("GET _local/ status: %d, _id: %v", code, getResult["_id"])
		assert.Equal(t, http.StatusOK, code, "doc created via _local/ PUT should be GETtable via _local/ GET")
	}
}
