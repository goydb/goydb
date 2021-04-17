package handler

import (
	"encoding/json"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

type DBSecurityPut struct {
	Base
}

func (s *DBSecurityPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.DB(w, r, db)); !ok {
		return
	}
	var sec model.Security
	err := json.NewDecoder(r.Body).Decode(&sec)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	err = db.PutSecurity(r.Context(), &sec)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
