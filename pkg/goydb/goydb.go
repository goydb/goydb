package goydb

import (
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/port"
)

type Goydb struct {
	*storage.Storage
	Handler http.Handler
	Config  *handler.ConfigStore
	Logger  port.Logger
}
