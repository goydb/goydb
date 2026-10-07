//go:build !notengo && nogoja

package goydb

import (
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/tengoview"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/port"
)

func init() {
	RegisterStorageOptionHook(func(logger port.Logger, cs *handler.ConfigStore) []storage.StorageOption {
		return []storage.StorageOption{
			storage.WithViewEngine("", tengoview.NewViewServer),
			storage.WithFilterEngine("", tengoview.NewFilterServer),
			storage.WithValidateEngine("", tengoview.NewValidateServerBuilder(logger.With("component", "validate"))),
			storage.WithUpdateEngine("", tengoview.NewUpdateServer),
		}
	})
}
