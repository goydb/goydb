package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// partitionPrefix returns the start/end key range for a given partition.
// Documents in a partition have IDs of the form "{partition}:{docid}".
func partitionPrefix(partition string) (startKey, endKey string) {
	return partition + ":", partition + ":\uffff"
}

// PartitionInfo handles GET /{db}/_partition/{partition}.
// Returns document count and sizes for the given partition.
type PartitionInfo struct {
	Base
}

func (s *PartitionInfo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	partition := pathVar(r, "partition")
	if partition == "" {
		WriteError(w, http.StatusBadRequest, "partition is required")
		return
	}

	startKey, endKey := partitionPrefix(partition)
	q := port.AllDocsQuery{
		StartKey:  startKey,
		EndKey:    endKey,
		SkipLocal: true,
	}

	docs, _, err := db.AllDocs(r.Context(), q)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	dbName := pathVar(r, "db")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"db_name":   dbName,
		"partition": partition,
		"doc_count": len(docs),
		"doc_del_count": 0,
		"sizes": map[string]int{
			"active":   0,
			"external": 0,
		},
	})
}

// PartitionAllDocs handles GET /{db}/_partition/{partition}/_all_docs.
// Lists all documents within a given partition.
type PartitionAllDocs struct {
	Base
}

func (s *PartitionAllDocs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	partition := pathVar(r, "partition")
	if partition == "" {
		WriteError(w, http.StatusBadRequest, "partition is required")
		return
	}

	options := r.URL.Query()
	pStart, pEnd := partitionPrefix(partition)

	var q port.AllDocsQuery
	q.Skip = intOption("skip", 0, options)
	q.Limit = intOption("limit", 0, options)
	q.SkipLocal = true
	q.IncludeDocs = boolOption("include_docs", false, options)

	// Allow user startkey/endkey within partition bounds.
	parseDocKeyRange(&q, options)
	if q.StartKey == "" || q.StartKey < pStart {
		q.StartKey = pStart
	}
	if q.EndKey == "" || q.EndKey > pEnd {
		q.EndKey = pEnd
	}
	q.Descending = boolOption("descending", false, options)

	docs, total, err := db.AllDocs(r.Context(), q)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows := formatDocRows(docs, q.IncludeDocs)

	response := AllDocsResponse{
		TotalRows: total,
		Rows:      rows,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response) //nolint:errcheck
}

// PartitionFind handles POST /{db}/_partition/{partition}/_find.
// Executes a Mango find query scoped to a partition.
type PartitionFind struct {
	Base
}

func (s *PartitionFind) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	partition := pathVar(r, "partition")
	if partition == "" {
		WriteError(w, http.StatusBadRequest, "partition is required")
		return
	}

	var body model.FindQuery
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	docs, stats, err := db.FindDocs(r.Context(), body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Filter results to only include documents in this partition.
	prefix := partition + ":"
	var result []map[string]interface{}
	for _, doc := range docs {
		if !strings.HasPrefix(doc.ID, prefix) {
			continue
		}
		d := doc.Data
		if d == nil {
			d = make(map[string]interface{})
		}
		d["_id"] = doc.ID
		d["_rev"] = doc.Rev
		result = append(result, d)
	}
	if result == nil {
		result = make([]map[string]interface{}, 0)
	}

	resp := map[string]interface{}{
		"docs": result,
	}
	if stats != nil {
		resp["execution_stats"] = stats
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// PartitionExplain handles POST /{db}/_partition/{partition}/_explain.
type PartitionExplain struct {
	Base
}

func (s *PartitionExplain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	partition := pathVar(r, "partition")

	var body struct {
		Selector map[string]interface{} `json:"selector"`
		Limit    int                    `json:"limit"`
		Skip     int                    `json:"skip"`
		Sort     json.RawMessage        `json:"sort,omitempty"`
		Fields   []string               `json:"fields,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if body.Limit == 0 {
		body.Limit = 25
	}

	dbName := pathVar(r, "db")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"dbname":    dbName,
		"partition": partition,
		"index": map[string]interface{}{
			"ddoc": nil,
			"name": "_all_docs",
			"type": "special",
			"def":  map[string]interface{}{"fields": []map[string]string{{"_id": "asc"}}},
		},
		"partitioned": true,
		"selector":    body.Selector,
		"opts": map[string]interface{}{
			"use_index":      []interface{}{},
			"bookmark":       "nil",
			"limit":          body.Limit,
			"skip":           body.Skip,
			"sort":           map[string]interface{}{},
			"fields":         body.Fields,
			"partition":      partition,
			"r":              []int{49},
			"conflicts":      false,
			"stale":          false,
			"update":         true,
			"stable":         false,
			"execution_stats": false,
		},
		"limit":  body.Limit,
		"skip":   body.Skip,
		"fields": body.Fields,
	})
}

// PartitionView handles GET /{db}/_partition/{partition}/_design/{ddoc}/_view/{view}.
// Queries a view scoped to a partition using startkey_docid/endkey_docid bounds.
type PartitionView struct {
	Base
}

func (s *PartitionView) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	partition := pathVar(r, "partition")
	ddoc := pathVar(r, "ddoc")
	view := pathVar(r, "view")

	if partition == "" || ddoc == "" || view == "" {
		WriteError(w, http.StatusBadRequest, "partition, ddoc, and view are required")
		return
	}

	// Set startkey_docid and endkey_docid to scope the view to the partition.
	q := r.URL.Query()
	if q.Get("startkey_docid") == "" {
		q.Set("startkey_docid", partition+":")
	}
	if q.Get("endkey_docid") == "" {
		q.Set("endkey_docid", partition+":\uffff")
	}
	r.URL.RawQuery = q.Encode()

	// Set the mux vars to point to the design doc and view.
	// Values must be percent-encoded because pathVar() will decode them.
	r = mux.SetURLVars(r, map[string]string{
		"db":    url.PathEscape(pathVar(r, "db")),
		"docid": url.PathEscape(ddoc),
		"view":  url.PathEscape(view),
	})

	// Delegate to the normal view handler.
	viewHandler := &DBView{Base: s.Base}
	viewHandler.ServeHTTP(w, r)
}
