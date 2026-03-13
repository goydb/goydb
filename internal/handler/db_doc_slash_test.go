package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
