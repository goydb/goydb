package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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

	session, ok := Authenticator{Base: s.Base}.DB(w, r, db)
	if !ok {
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if CheckMaxDocumentSize(w, s.Config, int64(len(bodyBytes))) {
		return
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &doc); err != nil {
		doc = make(map[string]interface{})
	}

	if CheckMaxDocsPerDB(w, s.Config, r.Context(), db, 1) {
		return
	}

	if CheckMaxDBSize(w, s.Config, r.Context(), db) {
		return
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	docID := hex.EncodeToString(b)
	doc["_id"] = docID

	mdoc := &model.Document{
		ID:   docID,
		Data: doc,
	}

	// Run validate_doc_update (POST is always a new document).
	if err := ValidateDocUpdate(r.Context(), db, s.Logger, mdoc, nil, session); err != nil {
		if writeValidationError(w, err) {
			return
		}
	}

	rev, err := db.PutDocument(r.Context(), mdoc)
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
