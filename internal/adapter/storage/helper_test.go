package storage

import (
	"context"
	"io/ioutil"
	"os"
	"testing"

	"github.com/d5/tengo/v2/require"
	"github.com/goydb/goydb/pkg/port"
	"github.com/stretchr/testify/assert"
)

func WithTestStorage(t *testing.T, fn func(ctx context.Context, s *Storage)) {
	ctx := context.Background()
	dir, err := ioutil.TempDir(os.TempDir(), "goydb-test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := Open(dir)
	assert.NoError(t, err)
	if err == nil {
		fn(ctx, s)
		s.Close()
	}
}

func WithTestDatabase(t *testing.T, fn func(ctx context.Context, db port.Database)) {
	WithTestStorage(t, func(ctx context.Context, s *Storage) {
		db, err := s.CreateDatabase(ctx, "test")
		assert.NoError(t, err)
		if err == nil {
			fn(ctx, db)
		}
	})
}
