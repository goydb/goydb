package handler

import (
	"net/http"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignDocs_PostKeys(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create design docs
	putDoc(t, c, "/testdb/_design/view1", map[string]interface{}{"views": map[string]interface{}{}})
	putDoc(t, c, "/testdb/_design/view2", map[string]interface{}{"views": map[string]interface{}{}})

	result, err := c.DB("testdb").DesignDocs(ctx, client.AllDocsQuery{Keys: []string{"_design/view1", "_design/view2"}})
	require.NoError(t, err)
	assert.Len(t, result.Rows, 2)
}

func TestDesignDocs_IncludeDocs(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDoc(t, c, "/testdb/_design/myview", map[string]interface{}{
		"views": map[string]interface{}{},
	})

	result, err := c.DB("testdb").DesignDocs(ctx, client.AllDocsQuery{IncludeDocs: true})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.NotNil(t, result.Rows[0].Doc)
}

func TestDesignDocs_UpdateSeq(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	code, result := getDoc(t, c, "/testdb/_design_docs?update_seq=true")
	assert.Equal(t, http.StatusOK, code)
	assert.NotNil(t, result["update_seq"])
}
