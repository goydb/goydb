package handler

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// DBDocsQueries handles POST /{db}/_all_docs/queries.
// Executes multiple _all_docs queries in a single request.
type DBDocsQueries struct {
	Base
}

func (s *DBDocsQueries) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var body struct {
		Queries []allDocsQueryParams `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	results := make([]AllDocsResponse, 0, len(body.Queries))
	for _, qp := range body.Queries {
		resp := executeAllDocsQuery(r, db, qp, false)
		results = append(results, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"results": results}) // nolint: errcheck
}

// DBDesignDocsQueries handles POST /{db}/_design_docs/queries.
// Executes multiple _design_docs queries in a single request.
type DBDesignDocsQueries struct {
	Base
}

func (s *DBDesignDocsQueries) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var body struct {
		Queries []allDocsQueryParams `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	results := make([]AllDocsResponse, 0, len(body.Queries))
	for _, qp := range body.Queries {
		resp := executeAllDocsQuery(r, db, qp, true)
		results = append(results, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"results": results}) // nolint: errcheck
}

type allDocsQueryParams struct {
	Keys         []string `json:"keys"`
	StartKey     string   `json:"startkey"`
	StartKeyAlt  string   `json:"start_key"`
	EndKey       string   `json:"endkey"`
	EndKeyAlt    string   `json:"end_key"`
	Key          string   `json:"key"`
	IncludeDocs  bool     `json:"include_docs"`
	Descending   bool     `json:"descending"`
	InclusiveEnd *bool    `json:"inclusive_end"`
	Limit        int      `json:"limit"`
	Skip         int      `json:"skip"`
}

func (qp allDocsQueryParams) toValues() url.Values {
	opts := make(url.Values)
	if qp.StartKey != "" {
		opts.Set("startkey", qp.StartKey)
	}
	if qp.StartKeyAlt != "" {
		opts.Set("start_key", qp.StartKeyAlt)
	}
	if qp.EndKey != "" {
		opts.Set("endkey", qp.EndKey)
	}
	if qp.EndKeyAlt != "" {
		opts.Set("end_key", qp.EndKeyAlt)
	}
	if qp.Key != "" {
		opts.Set("key", qp.Key)
	}
	if qp.InclusiveEnd != nil && !*qp.InclusiveEnd {
		opts.Set("inclusive_end", "false")
	}
	return opts
}

func executeAllDocsQuery(r *http.Request, db port.Database, qp allDocsQueryParams, designOnly bool) AllDocsResponse {
	ctx := r.Context()

	// Handle keys query
	if len(qp.Keys) > 0 {
		response := AllDocsResponse{
			TotalRows: len(qp.Keys),
			Rows:      make([]Rows, len(qp.Keys)),
		}
		for i, key := range qp.Keys {
			doc, err := db.GetDocument(ctx, key)
			if err != nil || doc == nil {
				response.Rows[i] = Rows{Key: key, Error: "not_found"}
				continue
			}
			response.Rows[i] = formatDocRow(doc, qp.IncludeDocs)
		}
		return response
	}

	opts := qp.toValues()

	q := port.AllDocsQuery{
		Skip:        int64(qp.Skip),
		Limit:       int64(qp.Limit),
		IncludeDocs: qp.IncludeDocs,
		Descending:  qp.Descending,
		SkipLocal:   true,
	}
	parseDocKeyRange(&q, opts)

	if designOnly {
		designStart := string(model.DesignDocPrefix)
		designEnd := string(model.DesignDocPrefix) + "香"
		if q.StartKey == "" {
			q.StartKey = designStart
		}
		if q.EndKey == "" {
			q.EndKey = designEnd
		}
	}

	docs, total, err := db.AllDocs(ctx, q)
	if err != nil {
		return AllDocsResponse{Rows: []Rows{}}
	}

	rows := formatDocRows(docs, qp.IncludeDocs)

	return AllDocsResponse{
		TotalRows: total,
		Rows:      rows,
	}
}
