package storage

import (
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// SearchIndexFactory creates a search index for a design document function.
type SearchIndexFactory func(ddfn *model.DesignDocFn, engines port.ViewEngines, path string, logger port.Logger) port.DocumentIndexSourceUpdate

var searchIndexFactory SearchIndexFactory

// RegisterSearchIndexFactory sets the global search index factory.
func RegisterSearchIndexFactory(f SearchIndexFactory) {
	searchIndexFactory = f
}
