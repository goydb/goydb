package replication

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStorage(t *testing.T) (*storage.Storage, func()) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-repl-test-*")
	require.NoError(t, err)

	s, err := storage.Open(dir)
	require.NoError(t, err)

	cleanup := func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
	return s, cleanup
}

func TestLocalDB_Head_Exists(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}
	assert.NoError(t, l.Head(ctx))
}

func TestLocalDB_Head_NotFound(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	l := &LocalDB{Storage: s, DBName: "nonexistent"}
	assert.Error(t, l.Head(context.Background()))
}

func TestLocalDB_GetDBInfo(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"foo": "bar"}})
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}
	info, err := l.GetDBInfo(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, info.UpdateSeq)
}

func TestLocalDB_GetChanges(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"val": i},
		})
		require.NoError(t, err)
	}

	l := &LocalDB{Storage: s, DBName: "testdb"}
	resp, err := l.GetChanges(ctx, "0", 10)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 5)
}

func TestLocalDB_GetChanges_WithSince(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"val": i},
		})
		require.NoError(t, err)
	}

	l := &LocalDB{Storage: s, DBName: "testdb"}
	// Get all changes first to find a since value
	allResp, err := l.GetChanges(ctx, "0", 10)
	require.NoError(t, err)
	require.Len(t, allResp.Results, 5)

	// Use seq of 3rd doc as since
	since := allResp.Results[2].Seq
	resp, err := l.GetChanges(ctx, since, 10)
	require.NoError(t, err)
	assert.True(t, len(resp.Results) < 5, "should have fewer than 5 results after since")
}

func TestLocalDB_RevsDiff_AllMissing(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}
	result, err := l.RevsDiff(ctx, map[string][]string{
		"doc1": {"1-abc"},
		"doc2": {"1-def"},
	})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Contains(t, result, "doc1")
	assert.Contains(t, result, "doc2")
}

func TestLocalDB_RevsDiff_SomePresent(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}
	result, err := l.RevsDiff(ctx, map[string][]string{
		"doc1": {rev},
		"doc2": {"1-def"},
	})
	require.NoError(t, err)
	assert.NotContains(t, result, "doc1")
	assert.Contains(t, result, "doc2")
}

func TestLocalDB_RevsDiff_NoneMissing(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}
	result, err := l.RevsDiff(ctx, map[string][]string{
		"doc1": {rev},
	})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestLocalDB_BulkDocs_NewEditsFalse(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}
	docs := []*model.Document{
		{ID: "doc1", Rev: "1-abc", Data: map[string]interface{}{"_id": "doc1", "_rev": "1-abc", "v": 1}},
		{ID: "doc2", Rev: "1-def", Data: map[string]interface{}{"_id": "doc2", "_rev": "1-def", "v": 2}},
	}
	err = l.BulkDocs(ctx, docs, false)
	require.NoError(t, err)

	// Verify docs exist with original revisions
	doc, err := l.GetDoc(ctx, "doc1", false, nil)
	require.NoError(t, err)
	assert.Equal(t, "1-abc", doc.Rev)
}

func TestLocalDB_GetDoc(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}
	doc, err := l.GetDoc(ctx, "doc1", true, nil)
	require.NoError(t, err)
	assert.Equal(t, "doc1", doc.ID)
	assert.NotEmpty(t, doc.Rev)
	assert.Contains(t, doc.Data, "_revisions")
}

func TestLocalDB_LocalDocRoundtrip(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	l := &LocalDB{Storage: s, DBName: "testdb"}

	doc := &model.Document{
		ID:   "_local/checkpoint-1",
		Data: map[string]interface{}{"_id": "_local/checkpoint-1", "seq": "42"},
	}
	err = l.PutLocalDoc(ctx, doc)
	require.NoError(t, err)

	got, err := l.GetLocalDoc(ctx, "checkpoint-1")
	require.NoError(t, err)
	assert.Equal(t, "42", got.Data["seq"])
}

func TestLocalDB_CreateDB(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	l := &LocalDB{Storage: s, DBName: "newdb"}
	err := l.CreateDB(ctx)
	require.NoError(t, err)

	assert.NoError(t, l.Head(ctx))
}
