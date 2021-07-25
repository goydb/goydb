package handler

import (
	"net/http"

	"github.com/gorilla/mux"
)

type DBDocAttachmentDelete struct {
	Base
}

func (s *DBDocAttachmentDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := mux.Vars(r)["docid"]
	attachment := mux.Vars(r)["attachment"]

	_, err := db.DeleteAttachment(r.Context(), docID, attachment)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`)) // nolint: errcheck
}
