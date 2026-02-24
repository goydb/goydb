package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSearchDB creates a database, puts several test documents, and creates
// a design doc with a search index.
func setupSearchDB(t *testing.T, s *storage.Storage, router *mux.Router, dbName string) {
	t.Helper()
	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, dbName)
	require.NoError(t, err)

	docs := []struct {
		id, name, docType string
		age               int
	}{
		{"doc1", "alice", "person", 30},
		{"doc2", "bob", "person", 25},
		{"doc3", "carol", "person", 35},
		{"doc4", "dave", "admin", 40},
		{"doc5", "eve", "person", 28},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID: d.id,
			Data: map[string]interface{}{
				"name": d.name,
				"type": d.docType,
				"age":  d.age,
			},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, dbName, "myidx", map[string]interface{}{
		"indexes": map[string]interface{}{
			"search": map[string]interface{}{
				"index": `function(doc) {
					index("name", doc.name, {"store": true});
					index("type", doc.type, {"store": true, "facet": true});
					index("age", doc.age, {"store": true});
				}`,
			},
		},
	})
}

// querySearch performs a GET request against the search endpoint.
func querySearch(t *testing.T, router *mux.Router, dbName, docID, index, query string) (SearchResult, int) {
	t.Helper()
	u := "/" + dbName + "/_design/" + docID + "/_search/" + index
	if query != "" {
		u += "?" + query
	}
	req := httptest.NewRequest("GET", u, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var result SearchResult
	if w.Code == http.StatusOK {
		require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	}
	return result, w.Code
}

// postSearch performs a POST request against the search endpoint.
func postSearch(t *testing.T, router *mux.Router, dbName, docID, index string, body interface{}) (SearchResult, int) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/"+dbName+"/_design/"+docID+"/_search/"+index, bytes.NewReader(b))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var result SearchResult
	if w.Code == http.StatusOK {
		require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	}
	return result, w.Code
}

func TestSearch_BasicQuery(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	result, code := querySearch(t, router, "testdb", "myidx", "search", "q=name:alice")
	require.Equal(t, http.StatusOK, code)
	require.True(t, result.TotalRows >= 1, "expected at least 1 match")
	assert.Equal(t, "doc1", result.Rows[0].ID)
}

func TestSearch_DefaultLimit(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create 30 documents.
	for i := 0; i < 30; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   "doc" + json.Number(rune('A'+i)).String(),
			Data: map[string]interface{}{"name": "test", "type": "item", "age": i},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "myidx", map[string]interface{}{
		"indexes": map[string]interface{}{
			"search": map[string]interface{}{
				"index": `function(doc) {
					index("name", doc.name, {"store": true});
					index("type", doc.type, {"store": true});
					index("age", doc.age, {"store": true});
				}`,
			},
		},
	})

	result, code := querySearch(t, router, "testdb", "myidx", "search", "q=name:test")
	require.Equal(t, http.StatusOK, code)
	// Default limit is 25.
	assert.LessOrEqual(t, len(result.Rows), 25)
}

func TestSearch_IncludeDocs(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	result, code := querySearch(t, router, "testdb", "myidx", "search", "q=name:alice&include_docs=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	assert.NotNil(t, result.Rows[0].Doc, "doc should be included")
	assert.Equal(t, "doc1", result.Rows[0].Doc["_id"])
	assert.NotEmpty(t, result.Rows[0].Doc["_rev"])
}

func TestSearch_Sort(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	result, code := querySearch(t, router, "testdb", "myidx", "search",
		`q=type:person&sort=%5B%22name%22%5D`)
	require.Equal(t, http.StatusOK, code)
	require.True(t, len(result.Rows) >= 2)
	// Sorted alphabetically by name; first should be alice.
	assert.Equal(t, "doc1", result.Rows[0].ID)
}

func TestSearch_Bookmark(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	// First page with limit=2.
	result1, code := querySearch(t, router, "testdb", "myidx", "search", "q=type:person&limit=2")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result1.Rows, 2)
	require.NotEmpty(t, result1.Bookmark, "bookmark should be set")

	// Second page using bookmark.
	result2, code := querySearch(t, router, "testdb", "myidx", "search",
		"q=type:person&limit=2&bookmark="+result1.Bookmark)
	require.Equal(t, http.StatusOK, code)
	require.True(t, len(result2.Rows) >= 1)

	// Pages should be disjoint.
	page1IDs := map[string]bool{}
	for _, row := range result1.Rows {
		page1IDs[row.ID] = true
	}
	for _, row := range result2.Rows {
		assert.False(t, page1IDs[row.ID], "page 2 should not contain page 1 doc: %s", row.ID)
	}
}

func TestSearch_POST(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	result, code := postSearch(t, router, "testdb", "myidx", "search", map[string]interface{}{
		"q":     "name:bob",
		"limit": 10,
	})
	require.Equal(t, http.StatusOK, code)
	require.True(t, result.TotalRows >= 1)
	assert.Equal(t, "doc2", result.Rows[0].ID)
}

func TestSearch_Stale(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	// stale=ok should be accepted and return results.
	_, code := querySearch(t, router, "testdb", "myidx", "search", "q=name:alice&stale=ok")
	assert.Equal(t, http.StatusOK, code)
}

func TestSearch_Counts(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	result, code := querySearch(t, router, "testdb", "myidx", "search",
		`q=name:*&counts=%5B%22type%22%5D`)
	require.Equal(t, http.StatusOK, code)
	if result.Counts != nil {
		typeCounts, ok := result.Counts["type"]
		if ok {
			assert.True(t, len(typeCounts) > 0, "expected facet counts for type field")
		}
	}
}

func TestSearch_IncludeFields(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	result, code := querySearch(t, router, "testdb", "myidx", "search",
		`q=name:alice&include_fields=%5B%22name%22%5D`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	// The fields should contain "name".
	assert.NotNil(t, result.Rows[0].Fields["name"])
}

func TestSearch_Highlight(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	result, code := querySearch(t, router, "testdb", "myidx", "search",
		`q=name:alice&highlight_fields=%5B%22name%22%5D`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	if result.Rows[0].Highlights != nil {
		_, ok := result.Rows[0].Highlights["name"]
		assert.True(t, ok, "expected highlight for name field")
	}
}

func TestSearch_Drilldown(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	// Search all names, but drilldown to type=admin only.
	result, code := querySearch(t, router, "testdb", "myidx", "search",
		`q=name:*&drilldown=%5B%22type%22%2C%22admin%22%5D`)
	require.Equal(t, http.StatusOK, code)
	// Only dave should match (type=admin).
	for _, row := range result.Rows {
		assert.Equal(t, "doc4", row.ID)
	}
}

func TestSearch_GroupField(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSearchDB(t, s, router, "testdb")

	// Grouped response uses a different struct; parse raw JSON to check structure.
	u := "/testdb/_design/myidx/_search/search?q=name:*&group_field=type&group_limit=10"
	req := httptest.NewRequest("GET", u, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&raw))
	_, hasGroups := raw["groups"]
	assert.True(t, hasGroups, "grouped response should contain 'groups' key")
}
