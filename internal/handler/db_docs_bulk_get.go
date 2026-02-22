package handler

import (
	"encoding/json"
	"net/http"
)

type DBDocsBulkGet struct {
	Base
}

type bulkGetRequest struct {
	Docs []bulkGetDoc `json:"docs"`
}

type bulkGetDoc struct {
	ID  string `json:"id"`
	Rev string `json:"rev,omitempty"`
}

func (s *DBDocsBulkGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var req bulkGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	type bulkGetResultEntry struct {
		ID   string                   `json:"id"`
		Docs []map[string]interface{} `json:"docs"`
	}

	results := make([]bulkGetResultEntry, len(req.Docs))
	for i, reqDoc := range req.Docs {
		entry := bulkGetResultEntry{ID: reqDoc.ID}
		doc, err := db.GetDocument(r.Context(), reqDoc.ID)
		if err != nil || doc == nil {
			entry.Docs = []map[string]interface{}{
				{
					"error": map[string]interface{}{
						"id":     reqDoc.ID,
						"rev":    "undefined",
						"error":  "not_found",
						"reason": "missing",
					},
				},
			}
		} else {
			entry.Docs = []map[string]interface{}{
				{"ok": doc.Data},
			}
		}
		results[i] = entry
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"results": results,
	})
}
