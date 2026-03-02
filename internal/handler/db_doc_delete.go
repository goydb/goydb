package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
)

type DBDocDelete struct {
	Base
	Design bool
	Local  bool
}

func (s *DBDocDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	session, ok := Authenticator{Base: s.Base}.DB(w, r, db)
	if !ok {
		return
	}

	docID := mux.Vars(r)["docid"]
	if s.Design {
		docID = string(model.DesignDocPrefix) + docID
	} else if s.Local {
		docID = string(model.LocalDocPrefix) + docID
	}
	query := r.URL.Query()
	rev := query.Get("rev")
	batch := query.Get("batch") == "ok"

	// Run validate_doc_update for non-local docs.
	if !s.Local && !isLocalDoc(docID) {
		var oldDoc *model.Document
		if existing, err := db.GetDocument(r.Context(), docID); err == nil && existing != nil && !existing.Deleted {
			oldDoc = existing
		}
		newDoc := &model.Document{
			ID:      docID,
			Deleted: true,
			Data:    map[string]interface{}{"_id": docID, "_deleted": true},
		}
		if err := ValidateDocUpdate(r.Context(), db, s.Logger, newDoc, oldDoc, session); err != nil {
			if writeValidationError(w, err) {
				return
			}
		}
	}

	dbdoc, err := db.DeleteDocument(r.Context(), docID, rev)
	if errors.Is(err, storage.ErrConflict) {
		WriteError(w, http.StatusConflict, err.Error())
		return
	} else if errors.Is(err, storage.ErrNotFound) {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var resp SimpleDocResponse
	resp.ID = dbdoc.ID
	resp.Rev = dbdoc.Rev
	resp.Ok = true

	w.Header().Set("Content-Type", "application/json")
	if batch {
		w.WriteHeader(http.StatusAccepted)
	}
	json.NewEncoder(w).Encode(resp) // nolint: errcheck
}

type SimpleDocResponse struct {
	ID     string `json:"id"`
	Ok     bool   `json:"ok"`
	Rev    string `json:"rev,omitempty"`
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}
