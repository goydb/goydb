package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBDocsAll struct {
	Base
	Local bool
}

func (s *DBDocsAll) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	options := r.URL.Query()
	var q port.AllDocsQuery
	q.Skip = intOption("skip", 0, options)
	q.Limit = intOption("limit", 0, options)
	q.SkipLocal = !s.Local
	if s.Local {
		q.StartKey = string(model.LocalDocPrefix)
		q.EndKey = string(model.LocalDocPrefix) + "香"
	} else {
		q.StartKey = strings.ReplaceAll(stringOption("startkey", "start_key", options), `"`, "")
		q.EndKey = strings.ReplaceAll(stringOption("endkey", "end_key", options), `"`, "")
	}
	q.IncludeDocs = boolOption("include_docs", false, options)

	docs, total, err := db.AllDocs(r.Context(), q)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := AllDocsResponse{
		TotalRows: total,
		Rows:      make([]Rows, len(docs)),
	}

	for i, doc := range docs {
		response.Rows[i].ID = doc.ID
		response.Rows[i].Key = doc.Key
		response.Rows[i].Value = Value{Rev: doc.Rev}
		response.Rows[i].Doc = doc.Data
		if response.Rows[i].Doc == nil {
			response.Rows[i].Doc = make(map[string]interface{})
		}
		response.Rows[i].Doc["_id"] = doc.ID
		response.Rows[i].Doc["_rev"] = doc.Rev
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type AllDocsResponse struct {
	TotalRows int    `json:"total_rows"`
	Offset    int    `json:"offset"`
	Rows      []Rows `json:"rows"`
}
type Value struct {
	Rev string `json:"rev"`
}
type Rows struct {
	ID    string                 `json:"id,omitempty"`
	Key   interface{}            `json:"key,omitempty"`
	Value interface{}            `json:"value,omitempty"`
	Doc   map[string]interface{} `json:"doc,omitempty"`
}
