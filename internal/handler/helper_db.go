package handler

import (
	"errors"
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/port"
)

type Database struct {
	Base
}

func (c Database) Do(w http.ResponseWriter, r *http.Request) port.Database {
	dbName := pathVar(r, "db")
	db, err := c.Storage.Database(r.Context(), dbName)
	if err != nil {
		if errors.Is(err, storage.ErrDatabaseUnavailable) {
			c.Logger.Warnf(r.Context(), "database unavailable", "dbName", dbName, "error", err)
			WriteError(w, http.StatusInternalServerError, "Database exists but could not be opened: "+err.Error())
			return nil
		}
		c.Logger.Warnf(r.Context(), "database not found", "dbName", dbName, "error", err)
		WriteError(w, http.StatusNotFound, "Database does not exist.")
		return nil
	}
	return db
}
