package handler

import (
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"

	"github.com/gorilla/sessions"
)

type Base struct {
	Storage      port.Storage
	SessionStore sessions.Store
	Admins       model.AdminUsers
	Config       *ConfigStore
	Replication  *service.Replication
	Logger       port.Logger
}
