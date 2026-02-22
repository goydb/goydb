package storage

import (
	"context"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRevsLimit_Default(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	limit, err := db.GetRevsLimit(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1000, limit)
}

func TestSetGetRevsLimit(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	require.NoError(t, db.SetRevsLimit(ctx, 500))

	limit, err := db.GetRevsLimit(ctx)
	require.NoError(t, err)
	assert.Equal(t, 500, limit)
}

func TestCompact_TrimsRevHistory(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a doc and update it 9 more times so RevHistory has 10 entries.
	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 0},
	})
	require.NoError(t, err)
	for i := 1; i < 10; i++ {
		rev, err = db.PutDocument(ctx, &model.Document{
			ID:   "doc1",
			Rev:  rev,
			Data: map[string]interface{}{"x": i},
		})
		require.NoError(t, err)
	}

	// Verify we have 10 entries before compaction.
	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.Len(t, doc.RevHistory, 10)

	// Set limit to 5 and compact.
	require.NoError(t, db.SetRevsLimit(ctx, 5))
	require.NoError(t, db.Compact(ctx))

	// After compaction RevHistory must be capped at 5.
	doc, err = db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	assert.Len(t, doc.RevHistory, 5)
}

func TestCompact_TrimsLeafRevHistory(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Build a replication doc with a long RevHistory via _revisions.
	ids := make([]interface{}, 10)
	for i := range ids {
		ids[i] = "hash" + string(rune('a'+i))
	}
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:  "doc1",
		Rev: "10-hasha",
		Data: map[string]interface{}{
			"_id":  "doc1",
			"_rev": "10-hasha",
			"_revisions": map[string]interface{}{
				"start": float64(10),
				"ids":   ids,
			},
		},
	})
	require.NoError(t, err)

	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	require.Len(t, leaves, 1)
	require.Len(t, leaves[0].RevHistory, 10)

	// Set limit to 4 and compact.
	require.NoError(t, db.SetRevsLimit(ctx, 4))
	require.NoError(t, db.Compact(ctx))

	// Leaf RevHistory must be trimmed.
	leaves, err = db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	require.Len(t, leaves, 1)
	assert.Len(t, leaves[0].RevHistory, 4)
}

func TestCompact_UnderLimitUnchanged(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Rev:  rev,
		Data: map[string]interface{}{"x": 2},
	})
	require.NoError(t, err)

	// 2 revisions, limit 10 — nothing should change.
	require.NoError(t, db.SetRevsLimit(ctx, 10))
	require.NoError(t, db.Compact(ctx))

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	assert.Len(t, doc.RevHistory, 2)
}
