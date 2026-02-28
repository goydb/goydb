package handler

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
)

type DBDocHead struct {
	Base
	Design bool
}

func (s *DBDocHead) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	docID := mux.Vars(r)["docid"]
	if s.Design {
		docID = string(model.DesignDocPrefix) + docID
	}

	revParam := r.URL.Query().Get("rev")

	doc, err := db.GetDocument(r.Context(), docID)
	if err != nil || doc == nil || doc.Deleted {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// If a specific revision was requested and it differs from the winner,
	// verify it exists in the leaf store.
	if revParam != "" && doc.Rev != revParam {
		leaf, leafErr := db.GetLeaf(r.Context(), docID, revParam)
		if leafErr != nil || leaf == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		doc = leaf
	}

	w.Header().Set("ETag", `"`+doc.Rev+`"`)
	w.Header().Set("X-Couch-Full-Commit", "true")
	w.WriteHeader(http.StatusOK)
}
