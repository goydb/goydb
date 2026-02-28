package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDoc_Latest(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create doc rev 1
	rev1 := createDoc(t, router, "testdb", "doc1")

	// Update to get rev 2
	code, result := putDoc(t, router, "/testdb/doc1", map[string]interface{}{
		"_id": "doc1", "_rev": rev1, "updated": true,
	})
	require.Equal(t, http.StatusCreated, code)
	rev2 := result["rev"].(string)

	// GET with rev=old&latest=true should return the latest (rev2)
	code, result = getDoc(t, router, "/testdb/doc1?rev="+rev1+"&latest=true")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, rev2, result["_rev"])
}

func TestGetDoc_AttachmentsInline(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello")

	code, result := getDoc(t, router, "/testdb/doc1?attachments=true")
	assert.Equal(t, http.StatusOK, code)

	atts, ok := result["_attachments"].(map[string]interface{})
	require.True(t, ok, "_attachments should be a map")
	att, ok := atts["file.txt"].(map[string]interface{})
	require.True(t, ok, "file.txt should be present")
	assert.NotEmpty(t, att["data"], "data should contain base64")
	assert.Equal(t, false, att["stub"])
}

func TestGetDoc_AttEncodingInfo(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDocAndAttachment(t, router, "testdb", "doc1", "file.txt", "hello")

	code, result := getDoc(t, router, "/testdb/doc1?att_encoding_info=true")
	assert.Equal(t, http.StatusOK, code)

	atts, ok := result["_attachments"].(map[string]interface{})
	require.True(t, ok)
	att, ok := atts["file.txt"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "identity", att["encoding"])
}

func TestGetDoc_Meta(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_ = createDoc(t, router, "testdb", "doc1")

	// meta=true should include _conflicts if any (empty is fine)
	code, _ := getDoc(t, router, "/testdb/doc1?meta=true")
	assert.Equal(t, http.StatusOK, code)
}
