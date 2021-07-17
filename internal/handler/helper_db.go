package handler

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
)

type Database struct {
	Base
}

func (c Database) Do(w http.ResponseWriter, r *http.Request) *storage.Database {
	dbName := mux.Vars(r)["db"]
	db, err := c.Storage.Database(r.Context(), dbName)
	if err != nil {
		WriteError(w, http.StatusNotFound, "Database does not exist.")
		return nil
	}
	return db
}
