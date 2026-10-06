//go:build sqlite

package goydb

import (
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/port"
)

func init() {
	handler.RegisterFeature("sqlite")

	// Defaults match sqlite's own hardcoded fallbacks exactly (see
	// internal/adapter/sqlite/options.go's defaultPoolConfig) — this
	// section only needs to exist so GET/PUT /_config/sqlite/{key} has
	// something to read and write; it changes no behavior until an operator
	// edits a value.
	handler.RegisterConfigDefaults(map[string]map[string]string{
		"sqlite": {
			"max_open_readers":    "0",
			"max_idle_readers":    "5",
			"reader_idle_timeout": "5m",
			"writer_idle_timeout": "5m",
		},
	})

	RegisterStorageOptionHook(func(logger port.Logger, cs *handler.ConfigStore) []storage.StorageOption {
		return []storage.StorageOption{
			storage.WithEngine("sqlite", storage.NewSQLiteEngineFactory(cs.Get, logger)),
		}
	})
}
