package handler

import (
	"encoding/json"
	"net/http"
)

type DBRevsLimitGet struct{ Base }
type DBRevsLimitPut struct{ Base }

func (s *DBRevsLimitGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}
	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	limit, err := db.GetRevsLimit(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(limit) //nolint:errcheck
}

func (s *DBRevsLimitPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}
	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.DB(w, r, db)); !ok {
		return
	}

	var limit int
	if err := json.NewDecoder(r.Body).Decode(&limit); err != nil || limit < 1 {
		WriteError(w, http.StatusBadRequest, "invalid revs_limit")
		return
	}
	if err := db.SetRevsLimit(r.Context(), limit); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
}
