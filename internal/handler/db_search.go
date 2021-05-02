package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
)

type DBSearch struct {
	Base
}

// https://docs.couchdb.org/en/latest/ddocs/search.html
func (s *DBSearch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := string(model.DesignDocPrefix) + mux.Vars(r)["docid"]
	index := mux.Vars(r)["index"]

	doc, err := db.GetDocument(r.Context(), docID)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	var res SearchResult

	// TODO: replace dummy data
	res.TotalRows = 1
	res.Rows = []*Rows{
		{
			ID:    "asdad",
			Key:   "name",
			Value: index,
			Doc:   doc.Data,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

type SearchResult struct {
	TotalRows int     `json:"total_rows"`
	Bookmark  string  `json:"bookmark"`
	Rows      []*Rows `json:"rows"`
}

type SearchRows struct {
	ID     string                 `json:"id"`
	Order  []float64              `json:"order"`
	Fields map[string]interface{} `json:"fields"`
}
