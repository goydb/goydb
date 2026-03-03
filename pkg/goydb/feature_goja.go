//go:build !nogoja

package goydb

import (
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/port"
)

func init() {
	handler.RegisterFeature("goja")
	RegisterStorageOptionHook(func(logger port.Logger) []storage.StorageOption {
		return []storage.StorageOption{
			storage.WithViewEngine("", gojaview.NewViewServer),
			storage.WithViewEngine("javascript", gojaview.NewViewServer),
			storage.WithFilterEngine("", gojaview.NewFilterServer),
			storage.WithFilterEngine("javascript", gojaview.NewFilterServer),
			storage.WithReducerEngine("", gojaview.NewReducerBuilder(logger.With("component", "reducer"))),
			storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(logger.With("component", "reducer"))),
			storage.WithValidateEngine("", gojaview.NewValidateServerBuilder(logger.With("component", "validate"))),
			storage.WithValidateEngine("javascript", gojaview.NewValidateServerBuilder(logger.With("component", "validate"))),
			storage.WithUpdateEngine("", gojaview.NewUpdateServer),
			storage.WithUpdateEngine("javascript", gojaview.NewUpdateServer),
		}
	})
}
