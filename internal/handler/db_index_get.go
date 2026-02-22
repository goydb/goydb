package handler

import (
	"encoding/json"
	"net/http"

	"github.com/goydb/goydb/internal/controller"
)

// DBIndexGet handles GET /{db}/_index — list Mango indexes.
type DBIndexGet struct {
	Base
}

func (s *DBIndexGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	indexes, err := controller.MangoIndex{DB: db}.List(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type indexEntry struct {
		Ddoc string                 `json:"ddoc"`
		Name string                 `json:"name"`
		Type string                 `json:"type"`
		Def  map[string]interface{} `json:"def"`
	}

	entries := make([]indexEntry, 0, len(indexes))
	for _, mi := range indexes {
		entry := indexEntry{
			Name: mi.Name,
		}
		if mi.Ddoc == "" {
			// Built-in _all_docs index
			entry.Ddoc = ""  // JSON null handled by omitempty would hide it; use "" here
			entry.Type = "special"
			var fields []map[string]string
			for _, f := range mi.Fields {
				fields = append(fields, map[string]string{f: "asc"})
			}
			entry.Def = map[string]interface{}{"fields": fields}
		} else {
			entry.Ddoc = mi.Ddoc
			entry.Type = "json"
			var fields []map[string]string
			for _, f := range mi.Fields {
				fields = append(fields, map[string]string{f: "asc"})
			}
			entry.Def = map[string]interface{}{"fields": fields}
		}
		entries = append(entries, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"total_rows": len(entries),
		"indexes":    entries,
	})
}
