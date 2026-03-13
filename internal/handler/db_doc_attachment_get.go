package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/goydb/goydb/pkg/model"
)

type DBDocAttachmentGet struct {
	Base
	Design bool
	Local  bool
}

func (s *DBDocAttachmentGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	// The rev query parameter is accepted for CouchDB compatibility.
	// In this implementation attachments are content-addressed, so
	// we always serve the winner's attachment.

	a, err := db.GetAttachment(r.Context(), docID, attachment)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	defer func() { _ = a.Reader.Close() }()

	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("ETag", fmt.Sprintf(`"md5-%s"`, a.Digest))

	// Use http.ServeContent to support Range requests (206 Partial Content).
	data, err := io.ReadAll(a.Reader)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.ServeContent(w, r, attachment, time.Time{}, bytes.NewReader(data))
}
