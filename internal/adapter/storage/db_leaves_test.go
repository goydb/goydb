package storage

import (
	"context"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLeaves_NoLeaves(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Document exists in docs bucket but no leaf was ever written (legacy).
	// GetLeaves should return empty without error.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	// Calling GetLeaf on a non-existent rev returns nil, nil.
	leaf, err := db.GetLeaf(ctx, "nonexistent", "1-bogus")
	assert.NoError(t, err)
	assert.Nil(t, leaf)
}

func TestGetLeaves_SingleLeaf(t *testing.T) {
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

	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	require.Len(t, leaves, 1)
	assert.Equal(t, rev, leaves[0].Rev)
	assert.Equal(t, "doc1", leaves[0].ID)
}

func TestGetLeaves_TwoConflicts(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz"},
	})
	require.NoError(t, err)

	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	assert.Len(t, leaves, 2)

	revs := make([]string, len(leaves))
	for i, l := range leaves {
		revs[i] = l.Rev
	}
	assert.ElementsMatch(t, []string{"1-aaa", "1-zzz"}, revs)
}

func TestGetLeaf_Found(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 42},
	})
	require.NoError(t, err)

	leaf, err := db.GetLeaf(ctx, "doc1", rev)
	require.NoError(t, err)
	require.NotNil(t, leaf)
	assert.Equal(t, rev, leaf.Rev)
	assert.Equal(t, "doc1", leaf.ID)
}

func TestGetLeaf_NotFound(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	// A non-existent rev returns nil, nil.
	leaf, err := db.GetLeaf(ctx, "doc1", "bogus-rev")
	assert.NoError(t, err)
	assert.Nil(t, leaf)
}
