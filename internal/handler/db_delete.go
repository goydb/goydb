package handler

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
)

type DBDelete struct {
	Base
}

func (s *DBDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	dbName := mux.Vars(r)["db"]

	err := s.Storage.DeleteDatabase(r.Context(), dbName)
	if errors.Is(err, storage.ErrUnknownDatabase) {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
