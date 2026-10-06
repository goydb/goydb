//go:build sqlite

package storage

import (
	"sync"

	"github.com/goydb/goydb/internal/adapter/storage/sqlite"
	"github.com/goydb/goydb/pkg/port"
)

// registerOptionalEngines registers "sqlite" as a selectable engine
// alongside the always-available "bbolt" (engine_bbolt.go) when built with
// `-tags sqlite`. This is the plain, zero-option baseline — used as-is by a
// library embedder calling storage.Open directly, bypassing pkg/goydb
// entirely — and overridden by NewSQLiteEngineFactory's config-aware
// version (via WithEngine) once a *handler.ConfigStore exists; see
// pkg/goydb/feature_sqlite.go.
func registerOptionalEngines(s *Storage) {
	s.registerEngine("sqlite", func(path string) (port.DatabaseEngine, error) {
		return sqlite.Open(path)
	})
}

// NewSQLiteEngineFactory builds an EngineFactory that reads SQLite
// connection-pool settings from configGet (matching a *handler.ConfigStore's
// Get method's signature — a plain function value, not the concrete type:
// internal/handler already imports this package, so accepting
// *handler.ConfigStore here would be an import cycle; see
// internal/adapter/logger.NewFromConfig for the same pattern solving the
// same problem) each time a database is opened, not once up front — so a
// newly-created or newly-reopened database picks up the latest config, even
// without restarting goydb. An already-open database's pool is not
// reconfigured retroactively.
//
// The returned factory owns a single *sync.Once (used to dedupe
// OptionsFromConfig's max_open_readers risk warning across however many
// databases it ends up opening) — a fresh one per factory instance, not a
// package-level global, so independent factories (e.g. in different tests,
// or a library embedder constructing more than one) never share dedup
// state they have no business sharing.
func NewSQLiteEngineFactory(configGet func(section, key string) (string, bool), logger port.Logger) EngineFactory {
	var warnOnce sync.Once
	return func(path string) (port.DatabaseEngine, error) {
		opts := sqlite.OptionsFromConfig(configGet, logger, &warnOnce)
		return sqlite.Open(path, opts...)
	}
}
