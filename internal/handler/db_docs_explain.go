package handler

import (
	"encoding/json"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

// DBDocsExplain handles POST /{db}/_explain.
// Returns the query plan that would be used for a Mango query.
type DBDocsExplain struct {
	Base
}

func (s *DBDocsExplain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var find model.FindQuery
	if err := json.NewDecoder(r.Body).Decode(&find); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if find.Limit == 0 {
		find.Limit = 25
	}

	// Default to the _all_docs special index
	indexInfo := map[string]interface{}{
		"ddoc": nil,
		"name": "_all_docs",
		"type": "special",
		"def":  map[string]interface{}{"fields": []interface{}{map[string]string{"_id": "asc"}}},
	}

	opts := map[string]interface{}{
		"use_index":  []interface{}{},
		"bookmark":   "nil",
		"limit":      find.Limit,
		"skip":       find.Skip,
		"sort":       find.Sort,
		"fields":     find.Fields,
		"conflicts":  find.Conflicts,
		"r":          []int{1},
		"stable":     find.Stable,
		"update":     true,
	}

	if find.Fields == nil {
		opts["fields"] = "all_fields"
	}
	if find.Sort == nil {
		opts["sort"] = map[string]interface{}{}
	}

	response := map[string]interface{}{
		"dbname":   db.Name(),
		"index":    indexInfo,
		"selector": find.Selector,
		"opts":     opts,
		"limit":    find.Limit,
		"skip":     find.Skip,
		"fields":   find.Fields,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response) // nolint: errcheck
}
