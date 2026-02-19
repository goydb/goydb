package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
)

type DBDocAttachmentHead struct {
	Base
	Design bool
	Local  bool
}

func (s *DBDocAttachmentHead) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := mux.Vars(r)["docid"]
	if s.Design {
		docID = string(model.DesignDocPrefix) + docID
	} else if s.Local {
		docID = string(model.LocalDocPrefix) + docID
	}
	attachment := mux.Vars(r)["attachment"]

	a, err := db.GetAttachment(r.Context(), docID, attachment)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	_ = a.Reader.Close()

	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("ETag", fmt.Sprintf(`"md5-%s"`, a.Digest))
	w.Header().Set("Content-Length", strconv.FormatInt(a.Length, 10))
	w.WriteHeader(http.StatusOK)
}
