package goydb

import (
	"net/http"

	"github.com/goydb/goydb/pkg/port"
)

type Goydb struct {
	port.Storage
	Handler http.Handler
}
