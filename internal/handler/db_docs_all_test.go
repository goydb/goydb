package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllDocs_UpdateSeq(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, router, "testdb", "doc1")

	req := httptest.NewRequest("GET", "/testdb/_all_docs?update_seq=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.NotNil(t, result["update_seq"], "update_seq should be present")
}

func TestAllDocs_ConflictsAccepted(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, router, "testdb", "doc1")

	// conflicts, attachments, att_encoding_info params should be accepted without error
	req := httptest.NewRequest("GET", "/testdb/_all_docs?conflicts=true&attachments=true&att_encoding_info=true&include_docs=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
