package handler

import (
	"net/http"

)

type DBHead struct {
	Base
}

func (s *DBHead) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	dbName := pathVar(r, "db")
	_, err := s.Storage.Database(r.Context(), dbName)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
