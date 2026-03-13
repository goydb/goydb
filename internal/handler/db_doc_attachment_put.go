package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

	docID := pathVar(r, "docid")
	if s.Design {
		docID = string(model.DesignDocPrefix) + docID
	} else if s.Local {
		docID = string(model.LocalDocPrefix) + docID
	}
	attachment := pathVar(r, "attachment")

	rev := revFromRequest(r)
	batch := r.URL.Query().Get("batch") == "ok"

	// Fast path: check Content-Length header against max_attachment_size.
	if r.ContentLength > 0 {
		if CheckMaxAttachmentSize(w, s.Config, r.ContentLength) {
			return
		}
	}

	if CheckMaxDBSize(w, s.Config, ctx, db) {
		return
	}

	// Wrap body with a limited reader if an attachment size limit is configured.
	attLimit := configInt64(s.Config, "couchdb", "max_attachment_size")
	body := newLimitedReadCloser(r.Body, attLimit)

	newRev, err := db.PutAttachment(ctx, docID, &model.Attachment{
		ContentType: r.Header.Get("Content-Type"),
		Filename:    attachment,
		Reader:      body,
		ExpectedRev: rev,
	})
	if errors.Is(err, ErrLimitExceeded) {
		WriteError(w, http.StatusRequestEntityTooLarge, "attachment_too_large")
		return
	}
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

	status := http.StatusCreated
	if batch {
		status = http.StatusAccepted
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
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
