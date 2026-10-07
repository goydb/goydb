package goydb

import (
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/port"
)

// storageOptionHook receives the config store too (not just the logger) so
// a feature's storage options can depend on runtime-editable _config values
// — e.g. feature_sqlite.go's pool-tuning settings. Safe to reference
// *handler.ConfigStore directly here: pkg/goydb already imports
// internal/handler.
type storageOptionHook func(logger port.Logger, cs *handler.ConfigStore) []storage.StorageOption

var storageOptionHooks []storageOptionHook

// RegisterStorageOptionHook adds a hook that provides additional storage
// options at database open time. Used by build-tagged feature files.
func RegisterStorageOptionHook(hook storageOptionHook) {
	storageOptionHooks = append(storageOptionHooks, hook)
}
