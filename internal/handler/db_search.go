package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
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

	_, err := db.GetDocument(r.Context(), docID)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	ddfn := model.DesignDocFn{
		Type:        model.SearchFn,
		DesignDocID: docID,
		FnName:      index,
	}

	opts := r.URL.Query()
	sr, err := db.SearchDocuments(r.Context(), &ddfn, &port.SearchQuery{
		Query: stringOption("q", "query", opts),
		Limit: int(intOption("limit", 100, opts)),
	})
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	var res SearchResult

	// TODO: replace dummy data
	res.TotalRows = sr.Total
	res.Rows = make([]SearchRow, len(sr.Records))
	for i, rec := range sr.Records {
		res.Rows[i].ID = rec.ID
		res.Rows[i].Order = rec.Order
		res.Rows[i].Fields = rec.Fields
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res) // nolint: errcheck
}

type SearchResult struct {
	TotalRows uint64      `json:"total_rows"`
	Bookmark  string      `json:"bookmark"`
	Rows      []SearchRow `json:"rows"`
}

type SearchRow struct {
	ID     string                 `json:"id"`
	Order  []float64              `json:"order"`
	Fields map[string]interface{} `json:"fields"`
}
