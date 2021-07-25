package handler

import (
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

type DBDocAttachmentGet struct {
	Base
}

func (s *DBDocAttachmentGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	a, err := db.GetAttachment(r.Context(), docID, attachment)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	defer a.Reader.Close()

	w.Header().Set("Content-Type", a.ContentType)
	io.Copy(w, a.Reader) // nolint: errcheck
}
