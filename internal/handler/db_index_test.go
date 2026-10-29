package handler

import (
	"fmt"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexPost_Create(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	resp, err := c.DB("testdb").CreateIndex(ctx, client.IndexDef{
		Index: map[string]interface{}{"fields": []string{"type"}},
		Name:  "by_type",
		Ddoc:  "mango",
		Type:  "json",
	})
	require.NoError(t, err)

	assert.Equal(t, "created", resp["result"])
	assert.Equal(t, "_design/mango", resp["id"])
	assert.Equal(t, "by_type", resp["name"])

	// Design doc should exist in DB
	db, err := s.Database(ctx, "testdb")
	require.NoError(t, err)
	doc, err := db.GetDocument(ctx, "_design/mango")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestIndexPost_Exists(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	idx := client.IndexDef{
		Index: map[string]interface{}{"fields": []string{"status"}},
		Name:  "by_status",
		Ddoc:  "mango",
		Type:  "json",
	}

	// First create → "created"
	resp, err := c.DB("testdb").CreateIndex(ctx, idx)
	require.NoError(t, err)
	require.Equal(t, "created", resp["result"])

	// Second identical create → "exists"
	resp2, err := c.DB("testdb").CreateIndex(ctx, idx)
	require.NoError(t, err)
	assert.Equal(t, "exists", resp2["result"])
}

func TestIndexGet_Empty(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	resp, err := c.DB("testdb").ListIndexes(ctx)
	require.NoError(t, err)
	// Only the built-in _all_docs index
	assert.Equal(t, float64(1), resp["total_rows"])
}

func TestIndexGet_WithIndex(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = c.DB("testdb").CreateIndex(ctx, client.IndexDef{
		Index: map[string]interface{}{"fields": []string{"type"}},
		Name:  "by_type",
		Ddoc:  "mango",
		Type:  "json",
	})
	require.NoError(t, err)

	resp, err := c.DB("testdb").ListIndexes(ctx)
	require.NoError(t, err)
	assert.Equal(t, float64(2), resp["total_rows"])
}

func TestIndexDelete_OK(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create index
	_, err = c.DB("testdb").CreateIndex(ctx, client.IndexDef{
		Index: map[string]interface{}{"fields": []string{"type"}},
		Name:  "by_type",
		Ddoc:  "mango",
		Type:  "json",
	})
	require.NoError(t, err)

	// Delete index
	err = c.DB("testdb").DeleteIndex(ctx, "mango", "by_type")
	require.NoError(t, err)

	// GET _index should only have built-in now
	resp, err := c.DB("testdb").ListIndexes(ctx)
	require.NoError(t, err)
	assert.Equal(t, float64(1), resp["total_rows"])
}

func TestIndexDelete_NotFound(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = c.DB("testdb").DeleteIndex(ctx, "nonexistent", "myindex")
	require.Error(t, err)
	assert.Equal(t, 404, statusCode(t, err, 200))
}

func TestFind_UsesMangoIndex(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create Mango index on "type" field
	_, err = c.DB("testdb").CreateIndex(ctx, client.IndexDef{
		Index: map[string]interface{}{"fields": []string{"type"}},
		Name:  "by_type",
		Ddoc:  "mango",
		Type:  "json",
	})
	require.NoError(t, err)

	// Insert some documents
	for i := 0; i < 5; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("animal_%d", i),
			Data: map[string]interface{}{"type": "animal", "name": fmt.Sprintf("creature_%d", i)},
		})
		require.NoError(t, err)
	}
	for i := 0; i < 3; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("plant_%d", i),
			Data: map[string]interface{}{"type": "plant", "name": fmt.Sprintf("flora_%d", i)},
		})
		require.NoError(t, err)
	}

	// Find animals
	findResp, err := c.DB("testdb").Find(ctx, client.FindQuery{
		Selector: map[string]interface{}{
			"type": map[string]interface{}{"$eq": "animal"},
		},
		ExecutionStats: true,
	})
	require.NoError(t, err)
	assert.Len(t, findResp.Docs, 5)
}
