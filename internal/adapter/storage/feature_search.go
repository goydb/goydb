//go:build !nosearch

package storage

import (
	"github.com/goydb/goydb/internal/adapter/index"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func init() {
	RegisterSearchIndexFactory(func(ddfn *model.DesignDocFn, engines port.ViewEngines, path string, logger port.Logger) port.DocumentIndexSourceUpdate {
		return index.NewExternalSearchIndex(ddfn, engines, path, logger)
	})
}
