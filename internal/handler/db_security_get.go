package handler

import (
	"encoding/json"
	"net/http"
)

type DBSecurityGet struct {
	Base
}

func (s *DBSecurityGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.DB(w, r, db)); !ok {
		return
	}

	sec, err := db.GetSecurity(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sec)
}
