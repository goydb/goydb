package handler

import (
	"encoding/json"
	"net/http"
)

// DBCompact handles POST /{db}/_compact — no-op for embedded storage.
type DBCompact struct {
	Base
}

func (s *DBCompact) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}
	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.DB(w, r, db)); !ok {
		return
	}

	if err := db.Compact(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) // nolint: errcheck
}

// DBViewCleanup handles POST /{db}/_view_cleanup — no-op for embedded storage.
type DBViewCleanup struct {
	Base
}

func (s *DBViewCleanup) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}
	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.DB(w, r, db)); !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) // nolint: errcheck
}
