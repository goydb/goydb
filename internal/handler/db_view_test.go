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

	s, err := storage.Open(dir,
		storage.WithLogger(logger.NewNoLog()),
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
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
