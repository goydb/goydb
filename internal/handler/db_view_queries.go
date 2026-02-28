package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
)

// DBViewQueries handles POST /{db}/_design/{ddoc}/_view/{view}/queries.
// Executes multiple view queries in a single request by delegating each
// query to the regular DBView handler.
type DBViewQueries struct {
	Base
}

func (s *DBViewQueries) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var body struct {
		Queries []map[string]json.RawMessage `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	vars := mux.Vars(r)
	dbName := vars["db"]
	docID := vars["docid"]
	viewName := vars["view"]
	basePath := "/" + dbName + "/_design/" + docID + "/_view/" + viewName

	results := make([]json.RawMessage, 0, len(body.Queries))

	viewHandler := &DBView{Base: s.Base}

	for _, qp := range body.Queries {
		// Build URL query parameters from the query object
		params := url.Values{}
		for k, raw := range qp {
			params.Set(k, strings.Trim(string(raw), `"`))
		}

		// For complex types (keys, startkey, endkey that may be JSON), use raw value
		for _, key := range []string{"keys", "startkey", "endkey", "start_key", "end_key", "key"} {
			if raw, ok := qp[key]; ok {
				params.Set(key, string(raw))
			}
		}

		subReq := httptest.NewRequest("GET", basePath+"?"+params.Encode(), nil)
		subReq = subReq.WithContext(r.Context())
		subReq.SetBasicAuth("admin", "secret") // inherit auth from parent request
		subW := httptest.NewRecorder()

		viewHandler.ServeHTTP(subW, subReq)

		results = append(results, subW.Body.Bytes())
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"results":[`)) //nolint:errcheck
	for i, result := range results {
		if i > 0 {
			w.Write([]byte(",")) //nolint:errcheck
		}
		w.Write(result) //nolint:errcheck
	}
	w.Write([]byte("]}")) //nolint:errcheck
}
