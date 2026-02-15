package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
)

type DBDocPost struct {
	Base
}

func (s *DBDocPost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var doc map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		doc = make(map[string]interface{})
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	docID := hex.EncodeToString(b)
	doc["_id"] = docID

	rev, err := db.PutDocument(r.Context(), &model.Document{
		ID:   docID,
		Data: doc,
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
