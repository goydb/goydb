package storage

import (
	"github.com/goydb/goydb/internal/adapter/storage/bbolt"
	"github.com/goydb/goydb/pkg/port"
)

// registerBuiltinEngines registers every storage engine compiled into this
// binary. bbolt is the always-available base engine (no build tag — unlike
// sqlite, it has no opt-in dependency cost); registerOptionalEngines
// (engine_sqlite.go / engine_sqlite_stub.go, gated by the `sqlite` build
// tag) registers any additional engines this binary was built with.
func registerBuiltinEngines(s *Storage) {
	s.registerEngine("bbolt", func(path string) (port.DatabaseEngine, error) {
		return bbolt.Open(path)
	})
	registerOptionalEngines(s)
}
