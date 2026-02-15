package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/mitchellh/mapstructure"
)

type DBDocPut struct {
	Base
}

func (s *DBDocPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var doc map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&doc)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	docID, docIDok := doc["_id"].(string)
	if !docIDok {
		// Fall back to the URL path variable when _id is not in the body
		if id, ok := mux.Vars(r)["docid"]; ok {
			if strings.Contains(r.URL.Path, "/_design/") {
				docID = string(model.DesignDocPrefix) + id
			} else if strings.Contains(r.URL.Path, "/_local/") {
				docID = "_local/" + id
			} else {
				docID = id
			}
		} else {
			WriteError(w, http.StatusBadRequest, "missing _id")
			return
		}
	}

	var attachments map[string]*model.Attachment
	err = mapstructure.Decode(doc["_attachments"], &attachments)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"ok":  true,
		"id":  docID,
		"rev": rev,
	})
}
