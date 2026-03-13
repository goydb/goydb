package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
)

type DBDocAttachmentDelete struct {
	Base
	Design bool
	Local  bool
}

func (s *DBDocAttachmentDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := pathVar(r, "docid")
	if s.Design {
		docID = string(model.DesignDocPrefix) + docID
	} else if s.Local {
		docID = string(model.LocalDocPrefix) + docID
	}
	attachment := pathVar(r, "attachment")

	rev := revFromRequest(r)
	batch := r.URL.Query().Get("batch") == "ok"

	newRev, err := db.DeleteAttachment(r.Context(), docID, attachment, rev)
	if errors.Is(err, storage.ErrConflict) {
		WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, storage.ErrNotFound) {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if batch {
		w.WriteHeader(http.StatusAccepted)
	}
	json.NewEncoder(w).Encode(SimpleDocResponse{Ok: true, ID: docID, Rev: newRev}) // nolint: errcheck
}
