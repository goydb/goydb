package handler

import (
	"net/http"

	"github.com/gorilla/mux"
)

type DBDocHead struct {
	Base
}

func (s *DBDocHead) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	docID := mux.Vars(r)["docid"]
	doc, err := db.GetDocument(r.Context(), docID)
	if err != nil || doc == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("ETag", `"`+doc.Rev+`"`)
	w.WriteHeader(http.StatusOK)
}
