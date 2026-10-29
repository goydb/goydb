package handler

import (
	"net/http"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createDoc(t *testing.T, c *client.Client, db, id string) string {
	t.Helper()
	code, result := putDoc(t, c, "/"+db+"/"+id, map[string]interface{}{"_id": id, "hello": "world"})
	require.Equal(t, http.StatusCreated, code)
	return result["rev"].(string)
}

func TestDeleteDoc_BatchOk(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	code, result := deleteDoc(t, c, "/testdb/doc1?rev="+rev+"&batch=ok")

	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}

func TestDeleteDoc_WithoutBatch(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev := createDoc(t, c, "testdb", "doc1")

	code, result := deleteDoc(t, c, "/testdb/doc1?rev="+rev)

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "doc1", result["id"])
	assert.NotEmpty(t, result["rev"])
}
