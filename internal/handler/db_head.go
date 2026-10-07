package handler

import (
	"errors"
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
)

type DBHead struct {
	Base
}

func (s *DBHead) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	dbName := pathVar(r, "db")
	_, err := s.Storage.Database(r.Context(), dbName)
	if err != nil {
		if errors.Is(err, storage.ErrDatabaseUnavailable) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
