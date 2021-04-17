package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/mitchellh/mapstructure"
)

type DBDocPut struct {
	Base
}

func (s *DBDocPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var doc map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&doc)
	docID, docIDok := doc["_id"].(string)
	if err != nil || !docIDok {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var attachments map[string]*model.Attachment
	mapstructure.Decode(doc["_attachments"], &attachments)

	rev, err := db.PutDocument(r.Context(), &model.Document{
		ID:          docID,
		Data:        doc,
		Deleted:     doc["_deleted"] == "true",
		Attachments: attachments,
	})
	if errors.Is(err, storage.ErrConflict) {
		WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	doc["_rev"] = rev

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}
