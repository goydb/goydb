package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// DBLocalDocsQueries handles POST /{db}/_local_docs/queries.
// It accepts {"queries": [...]} where each query object supports the same
// parameters as GET /_local_docs, and returns {"results": [...]}.
type DBLocalDocsQueries struct {
	Base
}

func (s *DBLocalDocsQueries) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var body struct {
		Queries []map[string]interface{} `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	type queryResult struct {
		TotalRows *int   `json:"total_rows"`
		Offset    *int   `json:"offset"`
		Rows      []Rows `json:"rows"`
	}

	results := make([]queryResult, len(body.Queries))
	for i, qm := range body.Queries {
		q := buildLocalDocsQuery(qm)

		docs, _, err := db.AllDocs(r.Context(), q)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		rows := formatDocRows(docs, q.IncludeDocs)
		results[i] = queryResult{Rows: rows}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"results": results,
	})
}

// buildLocalDocsQuery converts a query object from the queries request body
// into an AllDocsQuery scoped to _local/ documents.
func buildLocalDocsQuery(qm map[string]interface{}) port.AllDocsQuery {
	opts := make(url.Values)
	for k, v := range qm {
		switch val := v.(type) {
		case string:
			opts.Set(k, val)
		case bool:
			if val {
				opts.Set(k, "true")
			} else {
				opts.Set(k, "false")
			}
		case float64:
			opts.Set(k, fmt.Sprintf("%v", val))
		}
	}

	localStart := string(model.LocalDocPrefix)
	localEnd := string(model.LocalDocPrefix) + "香"

	var q port.AllDocsQuery
	q.Skip = intOption("skip", 0, opts)
	q.Limit = intOption("limit", 0, opts)
	q.IncludeDocs = boolOption("include_docs", false, opts)

	parseDocKeyRange(&q, opts)
	if q.StartKey == "" {
		q.StartKey = localStart
	}
	if q.EndKey == "" {
		q.EndKey = localEnd
	}
	q.Descending = boolOption("descending", false, opts)
	if q.Descending {
		userStart := stringOption("startkey", "start_key", opts)
		userEnd := stringOption("endkey", "end_key", opts)
		if userStart == "" && userEnd == "" {
			q.StartKey = localEnd
			q.EndKey = localStart
		}
	}

	return q
}
