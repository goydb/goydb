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
	defer r.Body.Close()

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
	rev := r.URL.Query().Get("rev")

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
	json.NewEncoder(w).Encode(resp) // nolint: errcheck
}

type SimpleDocResponse struct {
	ID  string `json:"id"`
	Ok  bool   `json:"ok"`
	Rev string `json:"rev"`
}
