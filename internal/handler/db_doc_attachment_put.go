package handler

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
)

type DBDocAttachmentPut struct {
	Base
	Design bool
}

func (s *DBDocAttachmentPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	_, err := db.PutAttachment(ctx, docID, &model.Attachment{
		ContentType: r.Header.Get("Content-Type"),
		Filename:    attachment,
		Reader:      r.Body,
	})
	if errors.Is(err, storage.ErrNotFound) {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
