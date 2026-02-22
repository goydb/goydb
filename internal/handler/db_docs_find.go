package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/goydb/goydb/pkg/model"
)

type DBDocsFind struct {
	Base
}

func (s *DBDocsFind) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

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
		s.Logger.Warnf(r.Context(), "failed to find docs", "error", err)
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

	if len(find.Fields) > 0 {
		for i, d := range response.Docs {
			response.Docs[i] = projectFields(d, find.Fields)
		}
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

// projectFields returns a new map containing only the requested fields.
// _id and _rev are always included. Nested fields (e.g. "author.name") are
// resolved one level deep and merged into a sub-map.
func projectFields(doc map[string]interface{}, fields []string) map[string]interface{} {
	out := map[string]interface{}{
		"_id":  doc["_id"],
		"_rev": doc["_rev"],
	}
	for _, f := range fields {
		if f == "_id" || f == "_rev" {
			continue
		}
		parts := strings.SplitN(f, ".", 2)
		if len(parts) == 1 {
			if v, ok := doc[f]; ok {
				out[f] = v
			}
		} else {
			if sub, ok := doc[parts[0]].(map[string]interface{}); ok {
				nested := projectFields(sub, []string{parts[1]})
				delete(nested, "_id")
				delete(nested, "_rev")
				if existing, ok := out[parts[0]].(map[string]interface{}); ok {
					for k, v := range nested {
						existing[k] = v
					}
				} else {
					out[parts[0]] = nested
				}
			}
		}
	}
	return out
}
