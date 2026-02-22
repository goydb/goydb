package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
)

type DBDocAttachmentPut struct {
	Base
	Design bool
	Local  bool
}

func (s *DBDocAttachmentPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	rev := revFromRequest(r)

	newRev, err := db.PutAttachment(ctx, docID, &model.Attachment{
		ContentType: r.Header.Get("Content-Type"),
		Filename:    attachment,
		Reader:      r.Body,
		ExpectedRev: rev,
	})
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
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(SimpleDocResponse{Ok: true, ID: docID, Rev: newRev}) // nolint: errcheck
}

// revFromRequest extracts the expected revision from the ?rev= query parameter
// or the If-Match header (stripping surrounding quotes).
func revFromRequest(r *http.Request) string {
	if rev := r.URL.Query().Get("rev"); rev != "" {
		return rev
	}
	if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
		return strings.Trim(ifMatch, `"`)
	}
	return ""
}
