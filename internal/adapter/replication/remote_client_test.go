package replication

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteClientHead_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	assert.NoError(t, c.Head(context.Background()))
}

func TestRemoteClientHead_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	assert.Error(t, c.Head(context.Background()))
}

func TestRemoteClientGetDBInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"db_name":    "testdb",
			"update_seq": "42",
		})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	info, err := c.GetDBInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "42", info.UpdateSeq)
}

func TestRemoteClientGetChanges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"seq": "1", "id": "doc1", "changes": []map[string]string{{"rev": "1-a"}}},
				{"seq": "2", "id": "doc2", "changes": []map[string]string{{"rev": "1-b"}}},
				{"seq": "3", "id": "doc3", "changes": []map[string]string{{"rev": "1-c"}}},
			},
			"last_seq": "3",
			"pending":  0,
		})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	resp, err := c.GetChanges(context.Background(), "", 100)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 3)
	assert.Equal(t, "3", resp.LastSeq)
}

func TestRemoteClientGetChanges_WithSince(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "5", r.URL.Query().Get("since"))
		_ = json.NewEncoder(w).Encode(model.ChangesResponse{LastSeq: "5"})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	_, err = c.GetChanges(context.Background(), "5", 100)
	require.NoError(t, err)
}

func TestRemoteClientRevsDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"doc1": map[string]interface{}{
				"missing": []string{"1-abc"},
			},
		})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	result, err := c.RevsDiff(context.Background(), map[string][]string{"doc1": {"1-abc"}})
	require.NoError(t, err)
	assert.Contains(t, result, "doc1")
	assert.Equal(t, []string{"1-abc"}, result["doc1"].Missing)
}

func TestRemoteClientRevsDiff_AllPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	result, err := c.RevsDiff(context.Background(), map[string][]string{"doc1": {"1-abc"}})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRemoteClientGetDoc_WithRevs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "true", r.URL.Query().Get("revs"))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"_id":  "doc1",
			"_rev": "1-abc",
			"name": "test",
		})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	doc, err := c.GetDoc(context.Background(), "doc1", true, nil)
	require.NoError(t, err)
	assert.Equal(t, "doc1", doc.ID)
	assert.Equal(t, "1-abc", doc.Rev)
}

func TestRemoteClientGetDoc_WithOpenRevs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		assert.Contains(t, r.URL.Query().Get("open_revs"), "1-abc")

		// CouchDB returns an array when open_revs is used with Accept: application/json
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"ok": map[string]interface{}{
					"_id":  "doc1",
					"_rev": "1-abc",
					"name": "test",
				},
			},
		})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	doc, err := c.GetDoc(context.Background(), "doc1", true, []string{"1-abc"})
	require.NoError(t, err)
	assert.Equal(t, "doc1", doc.ID)
	assert.Equal(t, "1-abc", doc.Rev)
	assert.Equal(t, "test", doc.Data["name"])
}

func TestRemoteClientBulkDocs_NewEditsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req) // nolint: errcheck
		assert.Equal(t, false, req["new_edits"])
		docs := req["docs"].([]interface{})
		assert.Len(t, docs, 2)
		json.NewEncoder(w).Encode([]interface{}{}) // nolint: errcheck
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	docs := []*model.Document{
		{ID: "doc1", Rev: "1-a", Data: map[string]interface{}{}},
		{ID: "doc2", Rev: "1-b", Data: map[string]interface{}{}},
	}
	err = c.BulkDocs(context.Background(), docs, false)
	require.NoError(t, err)
}

func TestRemoteClientCreateDB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	assert.NoError(t, c.CreateDB(context.Background()))
}

func TestRemoteClientBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "admin", user)
		assert.Equal(t, "secret", pass)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewRemoteClient("http://admin:secret@" + srv.Listener.Addr().String(), nil)
	require.NoError(t, err)
	assert.NoError(t, c.Head(context.Background()))
}

func TestRemoteClientCustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Basic c3lzdGVtbWdyOnNlY3JldA==", r.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom-Header"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, map[string]string{
		"Authorization":   "Basic c3lzdGVtbWdyOnNlY3JldA==",
		"X-Custom-Header": "custom-value",
	})
	require.NoError(t, err)
	assert.NoError(t, c.Head(context.Background()))
}

func TestRemoteClientGetLocalDoc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/_local/")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"_id":  "_local/checkpoint-1",
			"_rev": "0-1",
			"seq":  "42",
		})
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	doc, err := c.GetLocalDoc(context.Background(), "checkpoint-1")
	require.NoError(t, err)
	assert.Equal(t, "42", doc.Data["seq"])
}

func TestRemoteClientPutLocalDoc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/_local/")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) // nolint: errcheck
	}))
	defer srv.Close()

	c, err := NewRemoteClient(srv.URL, nil)
	require.NoError(t, err)
	doc := &model.Document{
		ID:   "_local/checkpoint-1",
		Data: map[string]interface{}{"seq": "42"},
	}
	assert.NoError(t, c.PutLocalDoc(context.Background(), doc))
}
