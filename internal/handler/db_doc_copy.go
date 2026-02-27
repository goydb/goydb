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

// DBDocCopy handles the COPY HTTP method for documents.
// It copies the source document to the destination specified in the
// Destination header.
type DBDocCopy struct {
	Base
	Design bool
	Local  bool
}

func (s *DBDocCopy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	// Resolve source document ID.
	srcID := mux.Vars(r)["docid"]
	if s.Design {
		srcID = string(model.DesignDocPrefix) + srcID
	} else if s.Local {
		srcID = string(model.LocalDocPrefix) + srcID
	}

	// Parse Destination header: "dest-id" or "dest-id?rev=X-Y"
	dest := r.Header.Get("Destination")
	if dest == "" {
		WriteError(w, http.StatusBadRequest, "Destination header is required")
		return
	}
	var destRev string
	if idx := strings.Index(dest, "?"); idx != -1 {
		query := dest[idx+1:]
		dest = dest[:idx]
		for _, param := range strings.Split(query, "&") {
			if strings.HasPrefix(param, "rev=") {
				destRev = param[4:]
			}
		}
	}

	// GET source document.
	srcDoc, err := db.GetDocument(r.Context(), srcID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if srcDoc == nil {
		WriteError(w, http.StatusNotFound, "source document not found")
		return
	}

	// Build new document with destination ID and source data.
	newData := make(map[string]interface{}, len(srcDoc.Data))
	for k, v := range srcDoc.Data {
		// Skip internal meta fields that belong to the source.
		if k == "_id" || k == "_rev" || k == "_local_seq" || k == "_conflicts" {
			continue
		}
		newData[k] = v
	}
	newData["_id"] = dest
	if destRev != "" {
		newData["_rev"] = destRev
	}

	newDoc := &model.Document{
		ID:   dest,
		Rev:  destRev,
		Data: newData,
	}

	rev, err := db.PutDocument(r.Context(), newDoc)
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
		"id":  dest,
		"rev": rev,
	})
}
