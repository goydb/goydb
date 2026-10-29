package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllDocs_UpdateSeq(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, c, "testdb", "doc1")

	// Database.AllDocs doesn't expose update_seq, so use Raw.
	resp, err := c.DB("testdb").Raw(ctx, http.MethodGet, "/_all_docs?update_seq=true", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotNil(t, result["update_seq"], "update_seq should be present")
}

func TestAllDocs_ConflictsAccepted(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, c, "testdb", "doc1")

	// conflicts, attachments, att_encoding_info params should be accepted without
	// error -- not exposed by Database.AllDocs, so use Raw.
	resp, err := c.DB("testdb").Raw(ctx, http.MethodGet, "/_all_docs?conflicts=true&attachments=true&att_encoding_info=true&include_docs=true", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
