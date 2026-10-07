package storage

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage/bbolt"
	"github.com/goydb/goydb/pkg/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateDatabase_UnknownEngineArgReturnsError(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	_, err := s.CreateDatabase(context.Background(), "testdb", map[string]string{"engine": "does-not-exist"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownEngine))
}

func TestCreateDatabase_EngineArgSelectsRegisteredEngine(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-engine-select-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	var usedPath string
	s, err := Open(dir,
		WithLogger(logger.NewNoLog()),
		WithEngine("fake", func(path string) (port.DatabaseEngine, error) {
			usedPath = path
			return bbolt.Open(path)
		}),
	)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.CreateDatabase(context.Background(), "testdb", map[string]string{"engine": "fake"})
	require.NoError(t, err)
	assert.Contains(t, usedPath, "testdb")
}

func TestCreateDatabase_NoEngineArgFallsBackToDefaultEngine(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-engine-default-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	var calledDefault bool
	s, err := Open(dir,
		WithLogger(logger.NewNoLog()),
		WithEngine("other", func(path string) (port.DatabaseEngine, error) {
			calledDefault = true
			return bbolt.Open(path)
		}),
		WithDefaultEngine("other"),
	)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.CreateDatabase(context.Background(), "testdb") // no args at all — stays variadic-compatible
	require.NoError(t, err)
	assert.True(t, calledDefault, "CreateDatabase with no args must use WithDefaultEngine's choice, not the hardcoded bbolt fallback")
}

func TestWithEngine_OverridesExistingRegistration(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-engine-override-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	var overrideCalled bool
	s, err := Open(dir,
		WithLogger(logger.NewNoLog()),
		// Re-registering "bbolt" itself must replace the builtin factory —
		// mirrors how pkg/goydb/feature_sqlite.go swaps the plain sqlite
		// baseline for a config-aware one via the same mechanism.
		WithEngine("bbolt", func(path string) (port.DatabaseEngine, error) {
			overrideCalled = true
			return bbolt.Open(path)
		}),
	)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.CreateDatabase(context.Background(), "testdb")
	require.NoError(t, err)
	assert.True(t, overrideCalled)
}
