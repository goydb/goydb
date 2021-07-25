package index_test

import (
	"context"
	"io/ioutil"
	"os"
	"testing"

	"github.com/d5/tengo/v2/require"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/stretchr/testify/assert"
)

func WithTestStorage(t *testing.T, fn func(ctx context.Context, s *storage.Storage)) {
	ctx := context.Background()
	dir, err := ioutil.TempDir(os.TempDir(), "goydb-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := storage.Open(dir)
	assert.NoError(t, err)
	if err == nil {
		fn(ctx, s)
		s.Close()
	}
}

func WithTestDatabase(t *testing.T, fn func(ctx context.Context, db *storage.Database)) {
	WithTestStorage(t, func(ctx context.Context, s *storage.Storage) {
		db, err := s.CreateDatabase(ctx, "test")
		assert.NoError(t, err)
		if err == nil {
			fn(ctx, db)
		}
	})
}
