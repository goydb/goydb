package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
)

type DBCreate struct {
	Base
}

func (s *DBCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	dbName := pathVar(r, "db")
	db, _ := s.Storage.Database(r.Context(), dbName)
	if db != nil {
		WriteError(w, http.StatusConflict, "Database already exists.")
		return
	}

	if CheckMaxDatabases(w, s.Config, r.Context(), s.Storage) {
		return
	}

	// Forward every query parameter as-is (e.g. a future "q"/"n"/"partitioned",
	// matching CouchDB's own PUT /{db} arguments) rather than picking out
	// individual ones by name, so new creation options never require a
	// handler change — only CreateDatabase's engine implementations need to
	// start looking at a new key.
	args := make(map[string]string, len(r.URL.Query()))
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			args[k] = v[0]
		}
	}
	if args["engine"] == "" {
		if def, ok := s.Config.Get("couchdb", "default_engine"); ok && def != "" {
			args["engine"] = def
		}
	}

	_, err := s.Storage.CreateDatabase(r.Context(), dbName, args)
	if errors.Is(err, storage.ErrUnknownEngine) {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) // nolint: errcheck
}
