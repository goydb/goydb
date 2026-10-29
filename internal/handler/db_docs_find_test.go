package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFind_FieldsProjection(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "Alice", "age": 30},
	})
	require.NoError(t, err)

	resp, err := c.DB("testdb").Find(ctx, client.FindQuery{
		Selector: map[string]interface{}{"name": "Alice"},
		Fields:   []string{"name"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Docs, 1)

	doc := resp.Docs[0]
	assert.Equal(t, "doc1", doc["_id"])
	assert.NotEmpty(t, doc["_rev"])
	assert.Equal(t, "Alice", doc["name"])
	assert.Nil(t, doc["age"], "age should not be projected")
}

func TestFind_FieldsProjection_Nested(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID: "doc1",
		Data: map[string]interface{}{
			"meta": map[string]interface{}{
				"score": 99,
				"tag":   "go",
			},
			"title": "Test",
		},
	})
	require.NoError(t, err)

	resp, err := c.DB("testdb").Find(ctx, client.FindQuery{
		Selector: map[string]interface{}{"title": "Test"},
		Fields:   []string{"meta.score"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Docs, 1)

	doc := resp.Docs[0]
	meta, ok := doc["meta"].(map[string]interface{})
	require.True(t, ok, "meta should be a map")
	assert.Equal(t, float64(99), meta["score"])
	assert.Nil(t, meta["tag"], "tag should not be projected")
	assert.Nil(t, doc["title"], "title should not be projected")
}

func TestFind_SortAsc(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, n := range []int{3, 1, 2} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", n),
			Data: map[string]interface{}{"n": n},
		})
		require.NoError(t, err)
	}

	resp, err := c.DB("testdb").Find(ctx, client.FindQuery{
		Selector: map[string]interface{}{"n": map[string]interface{}{"$gt": 0}},
		Sort:     []map[string]string{{"n": "asc"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.Docs, 3)

	assert.Equal(t, float64(1), resp.Docs[0]["n"])
	assert.Equal(t, float64(2), resp.Docs[1]["n"])
	assert.Equal(t, float64(3), resp.Docs[2]["n"])
}

func TestFind_SortDesc(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, n := range []int{3, 1, 2} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", n),
			Data: map[string]interface{}{"n": n},
		})
		require.NoError(t, err)
	}

	resp, err := c.DB("testdb").Find(ctx, client.FindQuery{
		Selector: map[string]interface{}{"n": map[string]interface{}{"$gt": 0}},
		Sort:     []map[string]string{{"n": "desc"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.Docs, 3)

	assert.Equal(t, float64(3), resp.Docs[0]["n"])
	assert.Equal(t, float64(2), resp.Docs[1]["n"])
	assert.Equal(t, float64(1), resp.Docs[2]["n"])
}

func TestFind_SortWithLimit(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Insert in storage order: 5, 1, 3, 2, 4
	for _, n := range []int{5, 1, 3, 2, 4} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", n),
			Data: map[string]interface{}{"n": n},
		})
		require.NoError(t, err)
	}

	resp, err := c.DB("testdb").Find(ctx, client.FindQuery{
		Selector: map[string]interface{}{"n": map[string]interface{}{"$gt": 0}},
		Sort:     []map[string]string{{"n": "asc"}},
		Limit:    2,
	})
	require.NoError(t, err)
	require.Len(t, resp.Docs, 2)

	// Should return the 2 smallest values (1, 2), not the first 2 scanned (5, 1 or similar).
	assert.Equal(t, float64(1), resp.Docs[0]["n"])
	assert.Equal(t, float64(2), resp.Docs[1]["n"])
}

func TestFind_UseIndex(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a Mango index on "type" field under ddoc "mango", name "by_type".
	_, err = c.DB("testdb").CreateIndex(ctx, client.IndexDef{
		Index: map[string]interface{}{"fields": []string{"type"}},
		Name:  "by_type",
		Ddoc:  "mango",
		Type:  "json",
	})
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"type": "widget"},
		})
		require.NoError(t, err)
	}

	// Query with use_index pointing at our mango index.
	resp, err := c.DB("testdb").Find(ctx, client.FindQuery{
		Selector:       map[string]interface{}{"type": "widget"},
		UseIndex:       []interface{}{"mango", "by_type"},
		ExecutionStats: true,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Docs, 3)

	// With execution_stats, total_keys_examined > 0 when the index is used.
	require.NotNil(t, resp.ExecutionStats)
	assert.Equal(t, 3, resp.ExecutionStats.TotalKeysExamined)
}

func TestFind_NoopParams(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	// r, q, stable, conflicts, update are accepted-but-ignored CouchDB params
	// not modeled by client.FindQuery; send via Raw to verify they don't
	// break the request.
	body, _ := json.Marshal(map[string]interface{}{
		"selector":  map[string]interface{}{"x": 1},
		"r":         1,
		"q":         1,
		"stable":    true,
		"conflicts": false,
		"update":    "true",
	})
	rawResp, err := c.DB("testdb").Raw(ctx, "POST", "/_find", bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer rawResp.Body.Close() //nolint:errcheck

	require.Equal(t, 200, rawResp.StatusCode)
	var findResp client.FindResponse
	require.NoError(t, json.NewDecoder(rawResp.Body).Decode(&findResp))
	assert.Len(t, findResp.Docs, 1)
}
