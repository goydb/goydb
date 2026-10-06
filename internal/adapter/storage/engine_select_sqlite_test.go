//go:build sqlite

package storage

import (
	"context"
	"os"
	"testing"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateDatabase_BboltAndSQLiteCoexistInSameDirectory is the actual
// point of the additive (not mutually-exclusive) engine registration this
// build tag enables: a single Storage can hold both a bbolt-backed and a
// sqlite-backed database side by side, each selected per-database via
// CreateDatabase's "engine" arg (mirroring PUT /{db}?engine=...), and
// ReloadDatabases must reopen each with the engine that actually wrote its
// file — not whichever is configured as the default.
func TestCreateDatabase_BboltAndSQLiteCoexistInSameDirectory(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-engine-coexist-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = s.CreateDatabase(ctx, "boltdb", map[string]string{"engine": "bbolt"})
	require.NoError(t, err)
	_, err = s.CreateDatabase(ctx, "litedb", map[string]string{"engine": "sqlite"})
	require.NoError(t, err)

	require.NoError(t, s.Close())

	// Reopen the whole Storage from disk — ReloadDatabases must route each
	// file to its matching engine purely from the file's own shape
	// (extensionless vs .sqlite3), independent of s.defaultEngine.
	s2, err := Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s2.Close()

	names, err := s2.Databases(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"boltdb", "litedb"}, names)

	boltDB, err := s2.Database(ctx, "boltdb")
	require.NoError(t, err)
	_, err = boltDB.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"a": 1}})
	require.NoError(t, err)

	liteDB, err := s2.Database(ctx, "litedb")
	require.NoError(t, err)
	_, err = liteDB.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"a": 1}})
	require.NoError(t, err)
}
