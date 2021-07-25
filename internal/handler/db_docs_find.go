package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

type DBDocsFind struct {
	Base
}

func (s *DBDocsFind) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	// decode find request
	var find model.FindQuery
	err := json.NewDecoder(r.Body).Decode(&find)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if find.Limit == 0 {
		find.Limit = 25
	}

	docs, stats, err := db.FindDocs(r.Context(), find)
	if err != nil {
		log.Println(err)
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// bookmark is simply the last document in the list
	var bookmark string
	if len(docs) > 0 {
		bookmark = docs[len(docs)-1].ID
	}

	response := FindResponse{
		ExecutionStats: stats,
		Docs:           make([]map[string]interface{}, len(docs)),
		Bookmark:       bookmark,
	}
	if !find.ExecutionStats {
		response.ExecutionStats = nil
	}

	for i, doc := range docs {
		response.Docs[i] = doc.Data
		if response.Docs[i] == nil {
			response.Docs[i] = make(map[string]interface{})
		}
		response.Docs[i]["_id"] = doc.ID
		response.Docs[i]["_rev"] = doc.Rev
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response) // nolint: errcheck
}

type FindResponse struct {
	Docs           []map[string]interface{} `json:"docs"`
	Bookmark       string                   `json:"bookmark,omitempty"`
	ExecutionStats *model.ExecutionStats    `json:"execution_stats"`
	Warning        string                   `json:"warning,omitempty"`
}
