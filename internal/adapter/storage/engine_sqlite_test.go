//go:build sqlite

package storage

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapterlogger "github.com/goydb/goydb/internal/adapter/logger"
)

func TestNewSQLiteEngineFactory_ReadsConfigFreshPerDatabase(t *testing.T) {
	dir := t.TempDir()

	var calls int
	get := func(section, key string) (string, bool) {
		calls++
		return "", false // defaults apply; only the call count matters here
	}

	factory := NewSQLiteEngineFactory(get, adapterlogger.NewNoLog())

	db1, err := factory(filepath.Join(dir, "db1"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db1.Delete() })

	callsAfterFirst := calls
	assert.Positive(t, callsAfterFirst, "opening the first database should have read config")

	db2, err := factory(filepath.Join(dir, "db2"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Delete() })

	assert.Greater(t, calls, callsAfterFirst,
		"opening a second database should read config again, not reuse a value baked in when the factory was constructed")
}
