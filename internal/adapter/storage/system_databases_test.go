package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureSystemDatabases_CreatesUsersDB(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()

	err := s.EnsureSystemDatabases(ctx)
	require.NoError(t, err)

	db, err := s.Database(ctx, "_users")
	require.NoError(t, err)
	assert.NotNil(t, db)
}

func TestEnsureSystemDatabases_SeedsAuthDesignDoc(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()

	err := s.EnsureSystemDatabases(ctx)
	require.NoError(t, err)

	db, err := s.Database(ctx, "_users")
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "_design/_auth")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.False(t, doc.Deleted)

	assert.Equal(t, "javascript", doc.Data["language"])
	vdu, ok := doc.Data["validate_doc_update"].(string)
	require.True(t, ok, "validate_doc_update should be a string")
	assert.Contains(t, vdu, "doc.type must be user")
}

func TestEnsureSystemDatabases_SetsEmptySecurity(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()

	err := s.EnsureSystemDatabases(ctx)
	require.NoError(t, err)

	db, err := s.Database(ctx, "_users")
	require.NoError(t, err)

	sec, err := db.GetSecurity(ctx)
	require.NoError(t, err)
	require.NotNil(t, sec)

	// CouchDB defaults: empty Members/Admins = any authenticated user may access.
	assert.Empty(t, sec.Members.Names)
	assert.Empty(t, sec.Members.Roles)
	assert.Empty(t, sec.Admins.Names)
	assert.Empty(t, sec.Admins.Roles)
}

func TestEnsureSystemDatabases_Idempotent(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()

	err := s.EnsureSystemDatabases(ctx)
	require.NoError(t, err)

	db, err := s.Database(ctx, "_users")
	require.NoError(t, err)

	doc1, err := db.GetDocument(ctx, "_design/_auth")
	require.NoError(t, err)
	require.NotNil(t, doc1)
	rev1 := doc1.Rev

	// Second call should not change the revision.
	err = s.EnsureSystemDatabases(ctx)
	require.NoError(t, err)

	doc2, err := db.GetDocument(ctx, "_design/_auth")
	require.NoError(t, err)
	require.NotNil(t, doc2)
	assert.Equal(t, rev1, doc2.Rev)
}

func TestEnsureSystemDatabases_RecreatesDeletedDesignDoc(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()

	err := s.EnsureSystemDatabases(ctx)
	require.NoError(t, err)

	db, err := s.Database(ctx, "_users")
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "_design/_auth")
	require.NoError(t, err)
	require.NotNil(t, doc)

	// Delete the design doc.
	_, err = db.DeleteDocument(ctx, "_design/_auth", doc.Rev)
	require.NoError(t, err)

	// Ensure should re-seed it.
	err = s.EnsureSystemDatabases(ctx)
	require.NoError(t, err)

	doc2, err := db.GetDocument(ctx, "_design/_auth")
	require.NoError(t, err)
	require.NotNil(t, doc2)
	assert.False(t, doc2.Deleted)
	assert.Equal(t, "javascript", doc2.Data["language"])
}
