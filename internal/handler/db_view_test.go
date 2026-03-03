//go:build !nogoja

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupViewTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	t.Helper()

	dir, err := os.MkdirTemp(os.TempDir(), "goydb-view-test-*")
	require.NoError(t, err)

	log := logger.NewNoLog()
	s, err := storage.Open(dir,
		storage.WithLogger(log),
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
		storage.WithReducerEngine("", gojaview.NewReducerBuilder(log)),
		storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(log)),
	)
	require.NoError(t, err)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:            s,
		SessionStore:       store,
		Admins:             model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication: &service.Replication{Storage: s, Logger: logger.NewNoLog()}, Logger: logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	taskCtx, cancelTasks := context.WithCancel(context.Background())
	tc := controller.Task{Storage: s}
	go tc.Run(taskCtx)

	return s, r, func() {
		cancelTasks()
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func putDesignDoc(t *testing.T, router *mux.Router, dbName, docID string, body interface{}) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("PUT", "/"+dbName+"/_design/"+docID, bytes.NewReader(b))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
}

func queryView(t *testing.T, router *mux.Router, dbName, docID, viewName, query string) (AllDocsResponse, int) {
	t.Helper()
	url := "/" + dbName + "/_design/" + docID + "/_view/" + viewName
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest("GET", url, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var result AllDocsResponse
	if w.Code == http.StatusOK {
		require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	}
	return result, w.Code
}

func TestView_MapOnly_ReduceFalse(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "alice", "type": "user"},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"name": "bob", "type": "user"},
	})
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "users", "by_name", "reduce=false")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 2, result.TotalRows)

	keys := make([]interface{}, len(result.Rows))
	for i, row := range result.Rows {
		keys[i] = row.Key
	}
	assert.ElementsMatch(t, []interface{}{"alice", "bob"}, keys)
}

func TestView_MapOnly_DefaultReduce(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "alice", "type": "user"},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"name": "bob", "type": "user"},
	})
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	// No reduce param — defaults to true, goes through None reducer path
	result, code := queryView(t, router, "testdb", "users", "by_name", "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 2, result.TotalRows)

	keys := make([]interface{}, len(result.Rows))
	for i, row := range result.Rows {
		keys[i] = row.Key
	}
	assert.ElementsMatch(t, []interface{}{"alice", "bob"}, keys)
}

func TestView_EmptyDatabase(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { emit(doc.name, null); }`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "users", "by_name", "reduce=false")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 0, result.TotalRows)
	assert.Empty(t, result.Rows)
}

func TestView_DocsAddedAfterDesignDoc(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "carol", "type": "user"},
	})
	require.NoError(t, err)

	result, code := queryView(t, router, "testdb", "users", "by_name", "reduce=false")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 1, result.TotalRows)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "carol", result.Rows[0].Key)
}

func TestView_ReduceCount(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, name := range []string{"alice", "bob", "carol"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   name,
			Data: map[string]interface{}{"name": name, "type": "user"},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"count_by_type": map[string]interface{}{
				"map":    `function(doc) { if (doc.type) { emit(doc.type, 1); } }`,
				"reduce": "_count",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "users", "count_by_type", "group=0")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 3, result.TotalRows)
	require.Len(t, result.Rows, 1)
	assert.EqualValues(t, int64(3), result.Rows[0].Value)
}

func TestView_ReduceSum_DefaultCollapsed(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for id, val := range map[string]int{"doc10": 1, "doc20": 2, "doc30": 3} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   id,
			Data: map[string]interface{}{"_id": id, "a": val},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "view_check", map[string]interface{}{
		"views": map[string]interface{}{
			"testview": map[string]interface{}{
				"map":    `function(doc) { emit(doc._id, doc.a); }`,
				"reduce": "_sum",
			},
		},
	})

	// No reduce param — default is reduce=true, group=false → single row with total
	result, code := queryView(t, router, "testdb", "view_check", "testview", "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 3, result.TotalRows)
	require.Len(t, result.Rows, 1)
	assert.Nil(t, result.Rows[0].Key)
	assert.EqualValues(t, int64(6), result.Rows[0].Value)
}

func TestView_ReduceSum_GroupTrue(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for id, val := range map[string]int{"doc10": 1, "doc20": 2, "doc30": 3} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   id,
			Data: map[string]interface{}{"_id": id, "a": val},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "view_check", map[string]interface{}{
		"views": map[string]interface{}{
			"testview": map[string]interface{}{
				"map":    `function(doc) { emit(doc._id, doc.a); }`,
				"reduce": "_sum",
			},
		},
	})

	// group=true → one row per unique key, each value is the per-key sum
	result, code := queryView(t, router, "testdb", "view_check", "testview", "group=true")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 3, result.TotalRows)
	assert.Len(t, result.Rows, 3)

	total := float64(0)
	for _, row := range result.Rows {
		assert.NotNil(t, row.Key)
		total += row.Value.(float64)
	}
	assert.InDelta(t, float64(6), total, 0.001)
}

func TestView_ArrayKeyStartEndKey(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Mirrors the real-world query:
	//   startkey=["freebsd","apps"]  endkey=["freebsd","apps",{}]
	// The map emits [os, category, host] triples.
	docs := []struct {
		id   string
		os   string
		cat  string
		host string
	}{
		{"h1", "freebsd", "apps", "alpha"},
		{"h2", "freebsd", "apps", "beta"},
		{"h3", "freebsd", "db", "gamma"},   // different category — must be excluded
		{"h4", "linux", "apps", "delta"},   // different os — must be excluded
		{"h5", "freebsd", "apps", "zeta"},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"os": d.os, "category": d.cat, "host": d.host},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "cfg", map[string]interface{}{
		"views": map[string]interface{}{
			"host_config": map[string]interface{}{
				"map": `function(doc) { emit([doc.os, doc.category, doc.host], null); }`,
			},
		},
	})

	// URL-encode: startkey=["freebsd","apps"]  endkey=["freebsd","apps",{}]
	result, code := queryView(t, router, "testdb", "cfg", "host_config",
		`reduce=false&startkey=%5B%22freebsd%22%2C%22apps%22%5D&endkey=%5B%22freebsd%22%2C%22apps%22%2C%7B%7D%5D`)
	require.Equal(t, http.StatusOK, code)

	// Only the 3 freebsd/apps hosts should be returned.
	require.Len(t, result.Rows, 3, "expected only freebsd/apps rows")
	for _, row := range result.Rows {
		arr, ok := row.Key.([]interface{})
		require.True(t, ok, "key should be an array")
		assert.Equal(t, "freebsd", arr[0])
		assert.Equal(t, "apps", arr[1])
	}
}

func TestView_ArrayKeyExactKey(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, host := range []string{"alpha", "beta"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   host,
			Data: map[string]interface{}{"os": "freebsd", "host": host},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "cfg", map[string]interface{}{
		"views": map[string]interface{}{
			"by_os": map[string]interface{}{
				"map": `function(doc) { emit([doc.os, doc.host], null); }`,
			},
		},
	})

	// key=["freebsd","alpha"] should return exactly one row.
	result, code := queryView(t, router, "testdb", "cfg", "by_os",
		`reduce=false&key=%5B%22freebsd%22%2C%22alpha%22%5D`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	arr, ok := result.Rows[0].Key.([]interface{})
	require.True(t, ok)
	assert.Equal(t, "freebsd", arr[0])
	assert.Equal(t, "alpha", arr[1])
}

func TestView_ArrayKeyWithNumbers(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Mirrors a real-world view that emits [date, lang, sortNum].
	// The JS engine stores sortNum as int64; the query sends JSON numbers
	// (parsed as float64).  CBOR encodes these differently so the iterator
	// byte-range must match.
	docs := []struct {
		id   string
		date string
		lang string
		typ  string
	}{
		{"np1", "2026-01-29", "de", "newspaper"},
		{"a1", "2026-01-29", "de", "article"},
		{"a2", "2026-01-29", "de", "article"},
		{"np2", "2026-01-29", "en", "newspaper"}, // different lang — excluded
		{"a3", "2026-01-30", "de", "article"},     // different date — excluded
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"date": d.date, "language": d.lang, "type": d.typ},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "by_date_lang", map[string]interface{}{
		"views": map[string]interface{}{
			"docs": map[string]interface{}{
				"map": `function(doc) {
					var sort = doc.type === "newspaper" ? 0 : 1;
					emit([doc.date, doc.language, sort], null);
				}`,
			},
		},
	})

	// startkey=["2026-01-29","de",0]  endkey=["2026-01-29","de",1]
	result, code := queryView(t, router, "testdb", "by_date_lang", "docs",
		`reduce=false&startkey=%5B%222026-01-29%22%2C%22de%22%2C0%5D&endkey=%5B%222026-01-29%22%2C%22de%22%2C1%5D`)
	require.Equal(t, http.StatusOK, code)

	require.Len(t, result.Rows, 3, "expected 3 rows for 2026-01-29/de (1 newspaper + 2 articles)")
}

func TestView_MultiEmitPerDocument(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// One document that emits three rows with the same key.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"tags": []interface{}{"a", "b", "c"}},
	})
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "tags", map[string]interface{}{
		"views": map[string]interface{}{
			"by_tag": map[string]interface{}{
				"map": `function(doc) {
					if (doc.tags) {
						for (var i = 0; i < doc.tags.length; i++) {
							emit(doc.tags[i], null);
						}
					}
				}`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "tags", "by_tag", "reduce=false")
	require.Equal(t, http.StatusOK, code)

	// All three emitted rows must survive — previously only the last was returned.
	assert.Equal(t, 3, result.TotalRows)
	require.Len(t, result.Rows, 3)

	keys := make([]interface{}, len(result.Rows))
	for i, row := range result.Rows {
		keys[i] = row.Key
	}
	assert.ElementsMatch(t, []interface{}{"a", "b", "c"}, keys)
}

func TestView_NonexistentView(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	_, code := queryView(t, router, "testdb", "users", "by_name", "")
	assert.Equal(t, http.StatusNotFound, code)
}

func TestView_IncludeDocs(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "alice", "age": 30, "type": "user"},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"name": "bob", "age": 25, "type": "user"},
	})
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, doc.age); } }`,
			},
		},
	})

	// Query without include_docs
	result, code := queryView(t, router, "testdb", "users", "by_name", "reduce=false")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 2, result.TotalRows)
	assert.Len(t, result.Rows, 2)

	// Verify we have key and value but no doc
	for _, row := range result.Rows {
		assert.NotNil(t, row.Key, "key should be present")
		assert.NotNil(t, row.Value, "value should be present")
		assert.Nil(t, row.Doc, "doc should not be included without include_docs=true")
	}

	// Query with include_docs=true
	result, code = queryView(t, router, "testdb", "users", "by_name", "reduce=false&include_docs=true")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 2, result.TotalRows)
	assert.Len(t, result.Rows, 2)

	// Verify we have full documents
	for _, row := range result.Rows {
		assert.NotNil(t, row.Key, "key should be present")
		assert.NotNil(t, row.Value, "value should be present")
		assert.NotNil(t, row.Doc, "doc should be included with include_docs=true")

		// Verify doc contains the full document data
		assert.NotNil(t, row.Doc["_id"], "doc should have _id")
		assert.NotNil(t, row.Doc["_rev"], "doc should have _rev")
		assert.NotNil(t, row.Doc["name"], "doc should have name field")
		assert.NotNil(t, row.Doc["age"], "doc should have age field")
		assert.Equal(t, "user", row.Doc["type"], "doc should have type field")
	}
}

// ---------------------------------------------------------------------------
// Reduce / rereduce / group_level comprehensive tests
// ---------------------------------------------------------------------------

func TestReduce_Count_GroupFalse(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, name := range []string{"alice", "bob", "carol"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   name,
			Data: map[string]interface{}{"type": "user"},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "stats", map[string]interface{}{
		"views": map[string]interface{}{
			"by_type": map[string]interface{}{
				"map":    `function(doc) { emit(doc.type, null); }`,
				"reduce": "_count",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "stats", "by_type", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	assert.Nil(t, result.Rows[0].Key)
	assert.EqualValues(t, int64(3), result.Rows[0].Value)
}

func TestReduce_Count_GroupTrue_StringKeys(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	docs := []struct{ id, typ string }{
		{"alice", "user"}, {"bob", "user"}, {"charlie", "user"}, {"dave", "admin"},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"type": d.typ},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "stats", map[string]interface{}{
		"views": map[string]interface{}{
			"by_type": map[string]interface{}{
				"map":    `function(doc) { emit(doc.type, null); }`,
				"reduce": "_count",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "stats", "by_type", "group=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2)
	// sorted by key: "admin" < "user"
	assert.Equal(t, "admin", result.Rows[0].Key)
	assert.EqualValues(t, int64(1), result.Rows[0].Value)
	assert.Equal(t, "user", result.Rows[1].Key)
	assert.EqualValues(t, int64(3), result.Rows[1].Value)
}

func TestReduce_Count_GroupTrue_ArrayKeys(t *testing.T) {
	// Bug 1 regression: array keys must not panic in the Count reducer.
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	docs := []struct{ id, os, cat string }{
		{"h1", "linux", "apps"},
		{"h2", "linux", "apps"},
		{"h3", "linux", "db"},
		{"h4", "freebsd", "apps"},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"os": d.os, "category": d.cat},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "cfg", map[string]interface{}{
		"views": map[string]interface{}{
			"host_count": map[string]interface{}{
				"map":    `function(doc) { emit([doc.os, doc.category], null); }`,
				"reduce": "_count",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "cfg", "host_count", "group=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 3)
	// sorted by CouchDB collation: ["freebsd","apps"] < ["linux","apps"] < ["linux","db"]
	assert.Equal(t, []interface{}{"freebsd", "apps"}, result.Rows[0].Key)
	assert.EqualValues(t, int64(1), result.Rows[0].Value)
	assert.Equal(t, []interface{}{"linux", "apps"}, result.Rows[1].Key)
	assert.EqualValues(t, int64(2), result.Rows[1].Value)
	assert.Equal(t, []interface{}{"linux", "db"}, result.Rows[2].Key)
	assert.EqualValues(t, int64(1), result.Rows[2].Value)
}

func TestReduce_Sum_GroupFalse(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, entry := range []struct {
		id string
		v  int
	}{{"d1", 10}, {"d2", 20}, {"d3", 30}} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   entry.id,
			Data: map[string]interface{}{"v": entry.v},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "sums", map[string]interface{}{
		"views": map[string]interface{}{
			"total": map[string]interface{}{
				"map":    `function(doc) { emit(null, doc.v); }`,
				"reduce": "_sum",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "sums", "total", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	assert.Nil(t, result.Rows[0].Key)
	assert.EqualValues(t, int64(60), result.Rows[0].Value)
}

func TestReduce_Sum_GroupTrue(t *testing.T) {
	// Verifies rows are returned sorted in CouchDB collation order.
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, e := range []struct {
		id, k string
		v     int
	}{{"d1", "c", 3}, {"d2", "a", 1}, {"d3", "b", 2}} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   e.id,
			Data: map[string]interface{}{"k": e.k, "v": e.v},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "sums", map[string]interface{}{
		"views": map[string]interface{}{
			"by_k": map[string]interface{}{
				"map":    `function(doc) { emit(doc.k, doc.v); }`,
				"reduce": "_sum",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "sums", "by_k", "group=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 3)
	// must be sorted: "a" < "b" < "c"
	assert.Equal(t, "a", result.Rows[0].Key)
	assert.EqualValues(t, int64(1), result.Rows[0].Value)
	assert.Equal(t, "b", result.Rows[1].Key)
	assert.EqualValues(t, int64(2), result.Rows[1].Value)
	assert.Equal(t, "c", result.Rows[2].Key)
	assert.EqualValues(t, int64(3), result.Rows[2].Value)
}

func TestReduce_Sum_FloatValues(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, e := range []struct {
		id    string
		score float64
	}{{"d1", 1.5}, {"d2", 2.5}, {"d3", 3.0}} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   e.id,
			Data: map[string]interface{}{"score": e.score},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "scores", map[string]interface{}{
		"views": map[string]interface{}{
			"total": map[string]interface{}{
				"map":    `function(doc) { emit("total", doc.score); }`,
				"reduce": "_sum",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "scores", "total", "group=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "total", result.Rows[0].Key)
	assert.InDelta(t, float64(7.0), result.Rows[0].Value, 0.001)
}

func TestReduce_GroupLevel1(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	docs := []struct{ id, os, cat, host string }{
		{"h1", "linux", "apps", "alpha"},
		{"h2", "linux", "db", "beta"},
		{"h3", "freebsd", "apps", "gamma"},
		{"h4", "freebsd", "apps", "delta"},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"os": d.os, "cat": d.cat, "host": d.host},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "cfg", map[string]interface{}{
		"views": map[string]interface{}{
			"host_count": map[string]interface{}{
				"map":    `function(doc) { emit([doc.os, doc.cat, doc.host], null); }`,
				"reduce": "_count",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "cfg", "host_count", "group_level=1")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2)
	// sorted: ["freebsd"] < ["linux"]
	assert.Equal(t, []interface{}{"freebsd"}, result.Rows[0].Key)
	assert.EqualValues(t, int64(2), result.Rows[0].Value)
	assert.Equal(t, []interface{}{"linux"}, result.Rows[1].Key)
	assert.EqualValues(t, int64(2), result.Rows[1].Value)
}

func TestReduce_GroupLevel2(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	docs := []struct{ id, os, cat, host string }{
		{"h1", "linux", "apps", "alpha"},
		{"h2", "linux", "db", "beta"},
		{"h3", "freebsd", "apps", "gamma"},
		{"h4", "freebsd", "apps", "delta"},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"os": d.os, "cat": d.cat, "host": d.host},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "cfg", map[string]interface{}{
		"views": map[string]interface{}{
			"host_count": map[string]interface{}{
				"map":    `function(doc) { emit([doc.os, doc.cat, doc.host], null); }`,
				"reduce": "_count",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "cfg", "host_count", "group_level=2")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 3)
	// sorted: ["freebsd","apps"] < ["linux","apps"] < ["linux","db"]
	assert.Equal(t, []interface{}{"freebsd", "apps"}, result.Rows[0].Key)
	assert.EqualValues(t, int64(2), result.Rows[0].Value)
	assert.Equal(t, []interface{}{"linux", "apps"}, result.Rows[1].Key)
	assert.EqualValues(t, int64(1), result.Rows[1].Value)
	assert.Equal(t, []interface{}{"linux", "db"}, result.Rows[2].Key)
	assert.EqualValues(t, int64(1), result.Rows[2].Value)
}

func TestReduce_GroupLevel_NonArray(t *testing.T) {
	// group_level with non-array keys should keep the full key (like group=true).
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, d := range []struct{ id, t string }{
		{"d1", "a"}, {"d2", "a"}, {"d3", "b"},
	} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"t": d.t},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "stats", map[string]interface{}{
		"views": map[string]interface{}{
			"by_type": map[string]interface{}{
				"map":    `function(doc) { emit(doc.t, null); }`,
				"reduce": "_count",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "stats", "by_type", "group_level=1")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2)
	assert.Equal(t, "a", result.Rows[0].Key)
	assert.EqualValues(t, int64(2), result.Rows[0].Value)
	assert.Equal(t, "b", result.Rows[1].Key)
	assert.EqualValues(t, int64(1), result.Rows[1].Value)
}

func TestReduce_Stats_Basic(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, e := range []struct {
		id string
		v  int
	}{{"d1", 1}, {"d2", 2}, {"d3", 3}} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   e.id,
			Data: map[string]interface{}{"v": e.v},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "stats", map[string]interface{}{
		"views": map[string]interface{}{
			"numbers": map[string]interface{}{
				"map":    `function(doc) { emit(null, doc.v); }`,
				"reduce": "_stats",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "stats", "numbers", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	stats, ok := result.Rows[0].Value.(map[string]interface{})
	require.True(t, ok, "stats value should be a map")
	assert.InDelta(t, float64(6), stats["sum"], 0.001)
	assert.InDelta(t, float64(3), stats["count"], 0.001)
	assert.InDelta(t, float64(1), stats["min"], 0.001)
	assert.InDelta(t, float64(3), stats["max"], 0.001)
	assert.InDelta(t, float64(14), stats["sumsqr"], 0.001)
}

func TestReduce_Stats_GroupTrue(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, d := range []struct {
		id, grp string
		v       int
	}{{"d1", "a", 1}, {"d2", "a", 3}, {"d3", "b", 5}} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"grp": d.grp, "v": d.v},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "stats", map[string]interface{}{
		"views": map[string]interface{}{
			"by_group": map[string]interface{}{
				"map":    `function(doc) { emit(doc.grp, doc.v); }`,
				"reduce": "_stats",
			},
		},
	})

	result, code := queryView(t, router, "testdb", "stats", "by_group", "group=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2)
	// sorted: "a" < "b"
	assert.Equal(t, "a", result.Rows[0].Key)
	statsA, ok := result.Rows[0].Value.(map[string]interface{})
	require.True(t, ok)
	assert.InDelta(t, float64(4), statsA["sum"], 0.001)
	assert.InDelta(t, float64(2), statsA["count"], 0.001)
	assert.Equal(t, "b", result.Rows[1].Key)
	statsB, ok := result.Rows[1].Value.(map[string]interface{})
	require.True(t, ok)
	assert.InDelta(t, float64(5), statsB["sum"], 0.001)
	assert.InDelta(t, float64(1), statsB["count"], 0.001)
}

func TestReduce_CustomJS_Sum(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, e := range []struct {
		id string
		v  int
	}{{"d1", 10}, {"d2", 20}, {"d3", 30}} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   e.id,
			Data: map[string]interface{}{"v": e.v},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "custom", map[string]interface{}{
		"views": map[string]interface{}{
			"total": map[string]interface{}{
				"map":    `function(doc) { emit(null, doc.v); }`,
				"reduce": `function(keys, values, rereduce) { return sum(values); }`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "custom", "total", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	assert.Nil(t, result.Rows[0].Key)
	assert.InDelta(t, float64(60), result.Rows[0].Value, 0.001)
}

func TestReduce_CustomJS_GroupTrue_ArrayKeys(t *testing.T) {
	// Bug 2 regression: array keys must not panic in gojaview reducer Result().
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	docs := []struct {
		id, os, cat string
		v           int
	}{
		{"h1", "linux", "apps", 1},
		{"h2", "linux", "apps", 2},
		{"h3", "freebsd", "db", 3},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"os": d.os, "cat": d.cat, "v": d.v},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "custom", map[string]interface{}{
		"views": map[string]interface{}{
			"by_os_cat": map[string]interface{}{
				"map":    `function(doc) { emit([doc.os, doc.cat], doc.v); }`,
				"reduce": `function(keys, values, rereduce) { return sum(values); }`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "custom", "by_os_cat", "group=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2)
	// sorted: ["freebsd","db"] < ["linux","apps"]
	assert.Equal(t, []interface{}{"freebsd", "db"}, result.Rows[0].Key)
	assert.InDelta(t, float64(3), result.Rows[0].Value, 0.001)
	assert.Equal(t, []interface{}{"linux", "apps"}, result.Rows[1].Key)
	assert.InDelta(t, float64(3), result.Rows[1].Value, 0.001)
}

func TestReduce_CustomJS_GroupLevel(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	docs := []struct {
		id, os, cat string
		v           int
	}{
		{"h1", "linux", "apps", 1},
		{"h2", "linux", "db", 2},
		{"h3", "freebsd", "apps", 4},
	}
	for _, d := range docs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   d.id,
			Data: map[string]interface{}{"os": d.os, "cat": d.cat, "v": d.v},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "custom", map[string]interface{}{
		"views": map[string]interface{}{
			"by_os_cat": map[string]interface{}{
				"map":    `function(doc) { emit([doc.os, doc.cat], doc.v); }`,
				"reduce": `function(keys, values, rereduce) { return sum(values); }`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "custom", "by_os_cat", "group_level=1")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2)
	// sorted: ["freebsd"] < ["linux"]
	assert.Equal(t, []interface{}{"freebsd"}, result.Rows[0].Key)
	assert.InDelta(t, float64(4), result.Rows[0].Value, 0.001)
	assert.Equal(t, []interface{}{"linux"}, result.Rows[1].Key)
	assert.InDelta(t, float64(3), result.Rows[1].Value, 0.001)
}

func TestReduce_ResponseOrdering(t *testing.T) {
	// Strings of different lengths: CBOR order (length-prefixed) diverges from
	// CouchDB collation (lexicographic).
	// CBOR order: "z"(1B) < "aa"(2B) < "bbb"(3B)
	// CouchDB order: "aa" < "bbb" < "z"
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, e := range []struct{ id, k string }{{"d1", "z"}, {"d2", "aa"}, {"d3", "bbb"}} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   e.id,
			Data: map[string]interface{}{"k": e.k},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "ord", map[string]interface{}{
		"views": map[string]interface{}{
			"by_k": map[string]interface{}{
				"map": `function(doc) { emit(doc.k, null); }`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "ord", "by_k", "reduce=false")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 3)
	assert.Equal(t, "aa", result.Rows[0].Key)
	assert.Equal(t, "bbb", result.Rows[1].Key)
	assert.Equal(t, "z", result.Rows[2].Key)
}

func TestReduce_Rereduce_Correctness(t *testing.T) {
	// Verifies that rereduce is NOT called for small datasets (< reduceOver
	// docs per key group), matching CouchDB's lazy-rereduce behaviour.
	//
	// The reducer returns values.length on first pass and sum(values) on
	// rereduce.  For 4 docs both paths yield 4, so the assertion is valid
	// regardless of which path is taken — what matters is that the result is
	// not empty/null (which would happen if rereduce were called spuriously
	// and the JS runtime handed the function stale intermediate state).
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, id := range []string{"d1", "d2", "d3", "d4"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   id,
			Data: map[string]interface{}{"type": "item"},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "custom", map[string]interface{}{
		"views": map[string]interface{}{
			"count": map[string]interface{}{
				"map": `function(doc) { emit(null, 1); }`,
				"reduce": `function(keys, values, rereduce) {
					if (rereduce) {
						return values.reduce(function(a, b) { return a + b; }, 0);
					}
					return values.length;
				}`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "custom", "count", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	assert.Nil(t, result.Rows[0].Key)
	assert.InDelta(t, float64(4), result.Rows[0].Value, 0.001)
}

func TestReduce_CustomJS_KeysPairsFormat(t *testing.T) {
	// Verifies that the reduce function receives CouchDB-compatible [key, docId]
	// pairs as its keys argument (first pass), not raw keys.
	// Reducers that use keys[i][1] to retrieve the doc ID will break if goydb
	// passes bare keys instead of [key, id] pairs.
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, id := range []string{"doc-a", "doc-b"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   id,
			Data: map[string]interface{}{"x": 1},
		})
		require.NoError(t, err)
	}

	// Reducer uses keys[i][1] to collect doc IDs — only works if keys is
	// [[key, id], ...] (CouchDB format), not [key, ...] (wrong format).
	putDesignDoc(t, router, "testdb", "custom", map[string]interface{}{
		"views": map[string]interface{}{
			"ids": map[string]interface{}{
				"map": `function(doc) { emit(null, 1); }`,
				"reduce": `function(keys, values, rereduce) {
					if (rereduce) { return values.reduce(function(a,b){return a+b;},0); }
					var ids = [];
					keys.forEach(function(pair) { ids.push(pair[1]); });
					return ids;
				}`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "custom", "ids", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 1)
	ids, ok := result.Rows[0].Value.([]interface{})
	require.True(t, ok, "value should be an array of doc IDs, got %T", result.Rows[0].Value)
	assert.Len(t, ids, 2)
}

// ---------------------------------------------------------------------------
// Helpers for new feature tests
// ---------------------------------------------------------------------------

// ViewTestResponse has pointer fields so callers can distinguish between
// "field absent from JSON" (nil) and "field present with zero value".
type ViewTestResponse struct {
	TotalRows *int             `json:"total_rows"`
	Offset    *int             `json:"offset"`
	UpdateSeq *json.RawMessage `json:"update_seq"`
	Rows      []Rows           `json:"rows"`
}

func postView(t *testing.T, router *mux.Router, dbName, docID, viewName string, body interface{}) (ViewTestResponse, int) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/"+dbName+"/_design/"+docID+"/_view/"+viewName, bytes.NewReader(b))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp ViewTestResponse
	if w.Code == http.StatusOK {
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	}
	return resp, w.Code
}

func queryViewFull(t *testing.T, router *mux.Router, dbName, docID, viewName, query string) (ViewTestResponse, int) {
	t.Helper()
	u := "/" + dbName + "/_design/" + docID + "/_view/" + viewName
	if query != "" {
		u += "?" + query
	}
	req := httptest.NewRequest("GET", u, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp ViewTestResponse
	if w.Code == http.StatusOK {
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	}
	return resp, w.Code
}

// setupSimpleViewDB creates a database named dbName with docs emitting
// (doc.name, null) from a "by_name" map view under design doc "users".
// Also adds a count reduce view "count_by_name".
func setupSimpleViewDB(t *testing.T, s *storage.Storage, router *mux.Router, dbName string, names []string) {
	t.Helper()
	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, dbName)
	require.NoError(t, err)
	for i, name := range names {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i+1),
			Data: map[string]interface{}{"name": name, "type": "user"},
		})
		require.NoError(t, err)
	}
	putDesignDoc(t, router, dbName, "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
			"count_by_name": map[string]interface{}{
				"map":    `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
				"reduce": "_count",
			},
		},
	})
}

// ---------------------------------------------------------------------------
// POST method tests
// ---------------------------------------------------------------------------

func TestView_POST_BasicParams(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob", "carol"})

	resp, code := postView(t, router, "testdb", "users", "by_name", map[string]interface{}{
		"reduce": false,
		"limit":  2,
	})
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, resp.Rows, 2)
}

func TestView_POST_AllQueryParams(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob", "carol", "dave"})

	// POST body: skip=1, reduce=false, include_docs=true
	resp, code := postView(t, router, "testdb", "users", "by_name", map[string]interface{}{
		"reduce":       false,
		"skip":         1,
		"include_docs": true,
	})
	require.Equal(t, http.StatusOK, code)
	// 4 docs, skip 1 → 3 rows (limit=100 default)
	assert.Len(t, resp.Rows, 3)
	for _, row := range resp.Rows {
		assert.NotNil(t, row.Doc, "include_docs=true should populate doc field")
	}
}

// ---------------------------------------------------------------------------
// keys parameter tests
// ---------------------------------------------------------------------------

func TestView_POST_KeysParam_MapOnly(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob", "carol"})

	resp, code := postView(t, router, "testdb", "users", "by_name", map[string]interface{}{
		"reduce": false,
		"keys":   []string{"bob", "alice"},
	})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Rows, 2)
	// Rows must be sorted by key in ascending order.
	assert.Equal(t, "alice", resp.Rows[0].Key)
	assert.Equal(t, "bob", resp.Rows[1].Key)
}

func TestView_POST_KeysParam_MissingKey(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob"})

	resp, code := postView(t, router, "testdb", "users", "by_name", map[string]interface{}{
		"reduce": false,
		"keys":   []string{"alice", "nonexistent"},
	})
	require.Equal(t, http.StatusOK, code)
	// Only alice should be in result; nonexistent produces no row.
	require.Len(t, resp.Rows, 1)
	assert.Equal(t, "alice", resp.Rows[0].Key)
}

func TestView_GET_KeysParam(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob", "carol"})

	resp, code := queryViewFull(t, router, "testdb", "users", "by_name",
		`reduce=false&keys=%5B%22bob%22%2C%22alice%22%5D`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Rows, 2)
	// Must be sorted ascending.
	assert.Equal(t, "alice", resp.Rows[0].Key)
	assert.Equal(t, "bob", resp.Rows[1].Key)
}

func TestView_POST_KeysParam_Reduce_Group(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "alice", "bob", "carol"})
	// Two docs with name "alice" (via two separate docs) — but wait, setupSimpleViewDB
	// uses doc IDs doc1, doc2, etc. Two "alice" docs won't happen with unique IDs
	// unless we specifically set names. Let's use a separate setup.
	// Actually the above already creates "alice" twice — but PutDocument with
	// different IDs is fine. Actually setupSimpleViewDB creates doc1=alice, doc2=alice, doc3=bob, doc4=carol.

	resp, code := postView(t, router, "testdb", "users", "count_by_name", map[string]interface{}{
		"group": true,
		"keys":  []string{"alice", "bob"},
	})
	require.Equal(t, http.StatusOK, code)
	// Should return grouped counts for alice and bob only (not carol).
	require.Len(t, resp.Rows, 2)
	// Sorted ascending: alice, bob
	assert.Equal(t, "alice", resp.Rows[0].Key)
	assert.Equal(t, "bob", resp.Rows[1].Key)
}

// ---------------------------------------------------------------------------
// Descending tests
// ---------------------------------------------------------------------------

func TestView_Descending_MapOnly(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob", "carol"})

	resp, code := queryViewFull(t, router, "testdb", "users", "by_name", "reduce=false&descending=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Rows, 3)
	// Keys must be in descending CouchDB collation order.
	assert.Equal(t, "carol", resp.Rows[0].Key)
	assert.Equal(t, "bob", resp.Rows[1].Key)
	assert.Equal(t, "alice", resp.Rows[2].Key)
}

func TestView_Descending_WithRange(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	// Use keys that are all the same byte-length to avoid CBOR collation issues.
	setupSimpleViewDB(t, s, router, "testdb", []string{"aaa", "bbb", "ccc", "ddd", "eee"})

	// startkey="ddd" endkey="bbb" descending=true → bbb, ccc, ddd (in descending order)
	resp, code := queryViewFull(t, router, "testdb", "users", "by_name",
		`reduce=false&descending=true&startkey=%22ddd%22&endkey=%22bbb%22`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Rows, 3, "expected 3 rows in [bbb,ddd] descending")
	assert.Equal(t, "ddd", resp.Rows[0].Key)
	assert.Equal(t, "ccc", resp.Rows[1].Key)
	assert.Equal(t, "bbb", resp.Rows[2].Key)
}

func TestView_Descending_Reduce_Group(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob", "carol"})

	resp, code := queryViewFull(t, router, "testdb", "users", "count_by_name",
		"group=true&descending=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Rows, 3)
	// Descending: carol, bob, alice
	assert.Equal(t, "carol", resp.Rows[0].Key)
	assert.Equal(t, "bob", resp.Rows[1].Key)
	assert.Equal(t, "alice", resp.Rows[2].Key)
}

// ---------------------------------------------------------------------------
// stale / stable tests
// ---------------------------------------------------------------------------

func TestView_Stale_OK(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob"})

	_, code := queryViewFull(t, router, "testdb", "users", "by_name", "reduce=false&stale=ok")
	assert.Equal(t, http.StatusOK, code)
}

func TestView_Stable(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob"})

	_, code := queryViewFull(t, router, "testdb", "users", "by_name", "reduce=false&stable=true")
	assert.Equal(t, http.StatusOK, code)
}

// ---------------------------------------------------------------------------
// sorted parameter tests
// ---------------------------------------------------------------------------

func TestView_Sorted_False(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob"})

	resp, code := queryViewFull(t, router, "testdb", "users", "by_name", "reduce=false&sorted=false")
	require.Equal(t, http.StatusOK, code)
	// total_rows and offset must be absent from the response.
	assert.Nil(t, resp.TotalRows, "total_rows should be absent when sorted=false")
	assert.Nil(t, resp.Offset, "offset should be absent when sorted=false")
	assert.Len(t, resp.Rows, 2)
}

func TestView_Sorted_True(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob"})

	resp, code := queryViewFull(t, router, "testdb", "users", "by_name", "reduce=false&sorted=true")
	require.Equal(t, http.StatusOK, code)
	// total_rows and offset must be present.
	assert.NotNil(t, resp.TotalRows, "total_rows should be present when sorted=true")
	assert.NotNil(t, resp.Offset, "offset should be present when sorted=true")
}

// ---------------------------------------------------------------------------
// update_seq tests
// ---------------------------------------------------------------------------

func TestView_UpdateSeq_True(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob"})

	resp, code := queryViewFull(t, router, "testdb", "users", "by_name", "reduce=false&update_seq=true")
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, resp.UpdateSeq, "update_seq should be present when update_seq=true")
	// Must be a numeric value > 0.
	var seq float64
	require.NoError(t, json.Unmarshal(*resp.UpdateSeq, &seq))
	assert.Greater(t, seq, float64(0))
}

func TestView_UpdateSeq_False(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	setupSimpleViewDB(t, s, router, "testdb", []string{"alice", "bob"})

	// Default (no update_seq param) should not include update_seq field.
	resp, code := queryViewFull(t, router, "testdb", "users", "by_name", "reduce=false")
	require.Equal(t, http.StatusOK, code)
	assert.Nil(t, resp.UpdateSeq, "update_seq should be absent by default")
}

// ---------------------------------------------------------------------------
// String key range query tests (CBOR collation fix)
// ---------------------------------------------------------------------------

func TestView_StringKeys_DifferentLengths_StartEndKey(t *testing.T) {
	// Regression test: CBOR encodes strings with a length prefix, so strings
	// sort by length first ("a" < "bb" < "cat" in CBOR), not lexicographically.
	// CouchDB sorts lexicographically: "a" < "bb" < "cat" < "d" < "elephant".
	// A range query startkey="bb"&endkey="d" must return "bb", "cat", "d".
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, name := range []string{"a", "bb", "cat", "d", "elephant"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   "doc-" + name,
			Data: map[string]interface{}{"name": name, "type": "user"},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	result, code := queryView(t, router, "testdb", "users", "by_name",
		`reduce=false&startkey=%22bb%22&endkey=%22d%22`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 3, "expected bb, cat, d")
	assert.Equal(t, "bb", result.Rows[0].Key)
	assert.Equal(t, "cat", result.Rows[1].Key)
	assert.Equal(t, "d", result.Rows[2].Key)
}

func TestView_StartEndKey_WithLimit(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, name := range []string{"a", "bb", "cat", "d", "elephant"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   "doc-" + name,
			Data: map[string]interface{}{"name": name, "type": "user"},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	// Range bb..d has 3 rows; limit=2 should return first 2.
	result, code := queryView(t, router, "testdb", "users", "by_name",
		`reduce=false&startkey=%22bb%22&endkey=%22d%22&limit=2`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2, "limit=2 should cap results")
	assert.Equal(t, "bb", result.Rows[0].Key)
	assert.Equal(t, "cat", result.Rows[1].Key)
}

func TestView_StartEndKey_WithSkip(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, name := range []string{"a", "bb", "cat", "d", "elephant"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   "doc-" + name,
			Data: map[string]interface{}{"name": name, "type": "user"},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	// Range bb..d has 3 rows (bb, cat, d); skip=1 should return cat, d.
	result, code := queryView(t, router, "testdb", "users", "by_name",
		`reduce=false&startkey=%22bb%22&endkey=%22d%22&skip=1`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2, "skip=1 should skip first result")
	assert.Equal(t, "cat", result.Rows[0].Key)
	assert.Equal(t, "d", result.Rows[1].Key)
}

func TestView_Reduce_StartEndKey_Descending(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for _, name := range []string{"a", "bb", "cat", "d", "elephant"} {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   "doc-" + name,
			Data: map[string]interface{}{"name": name, "type": "user"},
		})
		require.NoError(t, err)
	}

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"count_by_name": map[string]interface{}{
				"map":    `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
				"reduce": "_count",
			},
		},
	})

	// descending=true with startkey="d"&endkey="bb" → should return d, cat, bb in descending order.
	resp, code := queryViewFull(t, router, "testdb", "users", "count_by_name",
		`group=true&descending=true&startkey=%22d%22&endkey=%22bb%22`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Rows, 3, "expected d, cat, bb in descending order")
	assert.Equal(t, "d", resp.Rows[0].Key)
	assert.Equal(t, "cat", resp.Rows[1].Key)
	assert.Equal(t, "bb", resp.Rows[2].Key)
}

// ---------------------------------------------------------------------------
// Regression: include_docs with default reduce (None reducer path)
// ---------------------------------------------------------------------------

func TestView_IncludeDocs_DefaultReduce(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "alice", "type": "user"},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"name": "bob", "type": "user"},
	})
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	// Query with include_docs=true but WITHOUT reduce=false.
	// The default reduce=true path uses the None reducer for map-only views.
	result, code := queryView(t, router, "testdb", "users", "by_name", "include_docs=true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result.Rows, 2)

	for _, row := range result.Rows {
		assert.NotNil(t, row.Doc, "doc should be included with include_docs=true on default reduce path")
		assert.NotNil(t, row.Doc["_id"], "doc should have _id")
		assert.NotNil(t, row.Doc["_rev"], "doc should have _rev")
		assert.NotNil(t, row.Doc["name"], "doc should have name field")
	}
}

// ---------------------------------------------------------------------------
// Regression: null value must be present in JSON (not omitted)
// ---------------------------------------------------------------------------

func TestView_NullValue_Present(t *testing.T) {
	s, router, cleanup := setupViewTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"name": "alice", "type": "user"},
	})
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "users", map[string]interface{}{
		"views": map[string]interface{}{
			"by_name": map[string]interface{}{
				"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
			},
		},
	})

	// Query the view and check the raw JSON for "value": null.
	url := "/testdb/_design/users/_view/by_name?reduce=false"
	req := httptest.NewRequest("GET", url, nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, `"value":null`, "JSON must contain \"value\":null, not omit the field")
}
