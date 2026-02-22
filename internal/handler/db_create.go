package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type DBCreate struct {
	Base
}

func (s *DBCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	dbName := mux.Vars(r)["db"]
	db, _ := s.Storage.Database(r.Context(), dbName)
	if db != nil {
		WriteError(w, http.StatusConflict, "Database already exists.")
		return
	}

	_, err := s.Storage.CreateDatabase(r.Context(), dbName)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) // nolint: errcheck
}
