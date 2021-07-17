package handler

import (
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"

	"github.com/gorilla/sessions"
)

type Base struct {
	Storage      *storage.Storage
	SessionStore sessions.Store
	Admins       model.AdminUsers
}
