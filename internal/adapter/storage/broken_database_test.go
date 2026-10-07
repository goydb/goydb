package storage

import (
	"context"
	"errors"
	"os"
	"path"
	"testing"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpen_UnopenableDatabaseFileDoesNotAbortStartup is the core regression
// test: a single database file this binary can't open (e.g. a .sqlite3 file
// left behind by a binary built without -tags sqlite) must not take the
// whole server down. Every other database must still load, and the broken
// one must be reported rather than silently dropped.
func TestOpen_UnopenableDatabaseFileDoesNotAbortStartup(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-broken-db-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	// A .sqlite3 file on disk, but this Storage never registers a "sqlite"
	// engine factory -- simulates a binary not built with -tags sqlite,
	// without needing the real sqlite engine to reproduce the bug.
	require.NoError(t, os.WriteFile(path.Join(dir, "broken.sqlite3"), []byte("not a real db"), 0o600))
	require.NoError(t, os.WriteFile(path.Join(dir, "broken.sqlite3-wal"), []byte("wal"), 0o600))
	require.NoError(t, os.WriteFile(path.Join(dir, "broken.sqlite3-shm"), []byte("shm"), 0o600))

	s, err := Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err, "a single unopenable database file must not fail Storage.Open")
	defer s.Close()

	ctx := context.Background()

	// A normal (bbolt) database created afterwards must work fine --
	// the broken file must not have corrupted anything else.
	_, err = s.CreateDatabase(ctx, "healthy")
	require.NoError(t, err)

	names, err := s.Databases(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"broken", "healthy"}, names,
		"a database that exists but can't be opened must still be reported, like CouchDB/Fauxton do")

	_, err = s.Database(ctx, "broken")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDatabaseUnavailable))

	healthy, err := s.Database(ctx, "healthy")
	require.NoError(t, err)
	_, err = healthy.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"a": 1}})
	require.NoError(t, err)
}

func TestDeleteDatabase_RemovesBrokenDatabaseFilesAndFreesTheName(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-broken-db-delete-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	require.NoError(t, os.WriteFile(path.Join(dir, "broken.sqlite3"), []byte("not a real db"), 0o600))
	require.NoError(t, os.WriteFile(path.Join(dir, "broken.sqlite3-wal"), []byte("wal"), 0o600))

	s, err := Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()

	require.NoError(t, s.DeleteDatabase(ctx, "broken"))

	names, err := s.Databases(ctx)
	require.NoError(t, err)
	assert.Empty(t, names)

	_, err = os.Stat(path.Join(dir, "broken.sqlite3"))
	assert.True(t, os.IsNotExist(err), "main broken-database file should be removed")
	_, err = os.Stat(path.Join(dir, "broken.sqlite3-wal"))
	assert.True(t, os.IsNotExist(err), "sidecar files sharing the broken database's file name should be removed too")

	// The name must be free to reuse afterwards.
	_, err = s.CreateDatabase(ctx, "broken")
	require.NoError(t, err)
}
