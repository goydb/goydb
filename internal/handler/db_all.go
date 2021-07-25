package handler

import (
	"encoding/json"
	"net/http"
	"sort"
)

type DBAll struct {
	Base
}

func (s *DBAll) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	names, err := s.Storage.Databases(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Stable(sort.StringSlice(names))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(names) // nolint: errcheck
}
