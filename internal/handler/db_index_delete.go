package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/goydb/goydb/internal/controller"
)

// DBIndexDelete handles DELETE /{db}/_index/{ddoc}/json/{name}.
type DBIndexDelete struct {
	Base
}

func (s *DBIndexDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	ddoc := pathVar(r, "ddoc")
	name := pathVar(r, "name")

	// Caller passes the design doc name without the "_design/" prefix in the URL.
	// Strip it if it was included anyway.
	ddoc = strings.TrimPrefix(ddoc, "_design/")

	err := controller.MangoIndex{DB: db}.Delete(r.Context(), ddoc, name)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			WriteError(w, http.StatusNotFound, errMsg)
		} else {
			WriteError(w, http.StatusInternalServerError, errMsg)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) //nolint:errcheck
}
