package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/d5/tengo/v2/require"
	adapterlogger "github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/port"
	"github.com/stretchr/testify/assert"
)

func WithTestStorage(t *testing.T, fn func(ctx context.Context, s *storage.Storage)) {
	ctx := context.Background()
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir) //nolint:errcheck

	s, err := storage.Open(dir, storage.WithLogger(adapterlogger.NewNoLog()))
	assert.NoError(t, err)
	if err == nil {
		fn(ctx, s)
		_ = s.Close()
	}
}

func WithTestDatabase(t *testing.T, fn func(ctx context.Context, db port.Database)) {
	WithTestStorage(t, func(ctx context.Context, s *storage.Storage) {
		db, err := s.CreateDatabase(ctx, "test")
		assert.NoError(t, err)
		if err == nil {
			fn(ctx, db)
		}
	})
}
