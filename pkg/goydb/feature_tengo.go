//go:build !notengo

package goydb

import (
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/tengoview"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/port"
)

func init() {
	handler.RegisterFeature("tengo")
	RegisterStorageOptionHook(func(logger port.Logger) []storage.StorageOption {
		return []storage.StorageOption{
			storage.WithViewEngine("tengo", tengoview.NewViewServer),
			storage.WithFilterEngine("tengo", tengoview.NewFilterServer),
			storage.WithValidateEngine("tengo", tengoview.NewValidateServerBuilder(logger.With("component", "validate"))),
			storage.WithUpdateEngine("tengo", tengoview.NewUpdateServer),
		}
	})
}
