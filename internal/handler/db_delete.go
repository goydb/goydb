package handler

import (
	"net/http"

	"github.com/gorilla/mux"
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
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
