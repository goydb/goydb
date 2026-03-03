package goydb

import (
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/port"
)

type storageOptionHook func(logger port.Logger) []storage.StorageOption

var storageOptionHooks []storageOptionHook

// RegisterStorageOptionHook adds a hook that provides additional storage
// options at database open time. Used by build-tagged feature files.
func RegisterStorageOptionHook(hook storageOptionHook) {
	storageOptionHooks = append(storageOptionHooks, hook)
}
