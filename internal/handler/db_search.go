//go:build !nosearch

package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBSearch struct {
	Base
}

// https://docs.couchdb.org/en/latest/ddocs/search.html
func (s *DBSearch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := string(model.DesignDocPrefix) + mux.Vars(r)["docid"]
	index := mux.Vars(r)["index"]

	_, err := db.GetDocument(r.Context(), docID)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	ddfn := model.DesignDocFn{
		Type:        model.SearchFn,
		DesignDocID: docID,
		FnName:      index,
	}

	// Merge URL query params with JSON body (POST support).
	opts := mergeBodyIntoOptions(r, r.URL.Query())

	sq := buildSearchQuery(opts)

	// stale handling: if stale != "ok", wait for pending index tasks.
	if sq.Stale != "ok" {
		for {
			n, err := db.TaskCount(r.Context())
			if err != nil {
				s.Logger.Errorf(r.Context(), "failed to get task count", "error", err)
				WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if n == 0 {
				break
			}
			time.Sleep(time.Second)
		}
	}

	sr, err := db.SearchDocuments(r.Context(), &ddfn, sq)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	// include_docs enrichment
	if sq.IncludeDocs && len(sr.Records) > 0 {
		docs := make([]*model.Document, len(sr.Records))
		for i, rec := range sr.Records {
			docs[i] = &model.Document{ID: rec.ID}
		}
		if enrichErr := db.EnrichDocuments(r.Context(), docs); enrichErr != nil {
			WriteError(w, http.StatusInternalServerError, enrichErr.Error())
			return
		}
		for i, doc := range docs {
			if doc.Data != nil {
				sr.Records[i].Doc = doc.Data
				sr.Records[i].Doc["_id"] = doc.ID
				sr.Records[i].Doc["_rev"] = doc.Rev
			}
		}
	}

	// Build response.
	if len(sr.Groups) > 0 {
		// Grouped response.
		groups := make([]SearchGroup, len(sr.Groups))
		for i, g := range sr.Groups {
			rows := make([]SearchRow, len(g.Rows))
			for j, rec := range g.Rows {
				rows[j] = searchRecordToRow(rec)
			}
			groups[i] = SearchGroup{
				By:        g.By,
				TotalRows: g.TotalRows,
				Rows:      rows,
			}
		}
		res := SearchResultGrouped{
			TotalRows: sr.Total,
			Bookmark:  sr.Bookmark,
			Groups:    groups,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res) // nolint: errcheck
		return
	}

	var res SearchResult
	res.TotalRows = sr.Total
	res.Bookmark = sr.Bookmark
	res.Rows = make([]SearchRow, len(sr.Records))
	for i, rec := range sr.Records {
		res.Rows[i] = searchRecordToRow(rec)
	}
	if sr.Counts != nil {
		res.Counts = sr.Counts
	}
	if sr.Ranges != nil {
		res.Ranges = sr.Ranges
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res) // nolint: errcheck
}

func searchRecordToRow(rec *port.SearchRecord) SearchRow {
	row := SearchRow{
		ID:     rec.ID,
		Order:  rec.Order,
		Fields: rec.Fields,
	}
	if rec.Doc != nil {
		row.Doc = rec.Doc
	}
	if len(rec.Highlights) > 0 {
		row.Highlights = rec.Highlights
	}
	return row
}

// buildSearchQuery parses all CouchDB search parameters from URL values.
func buildSearchQuery(opts url.Values) *port.SearchQuery {
	sq := &port.SearchQuery{
		Query: unquoteJSON(stringOption("q", "query", opts)),
		Limit: int(intOption("limit", 25, opts)),
	}

	sq.Bookmark = unquoteJSON(opts.Get("bookmark"))
	sq.Stale = unquoteJSON(opts.Get("stale"))
	sq.IncludeDocs = boolOption("include_docs", false, opts)
	sq.GroupField = unquoteJSON(opts.Get("group_field"))
	sq.GroupLimit = int(intOption("group_limit", 1, opts))
	sq.HighlightPreTag = unquoteJSON(opts.Get("highlight_pre_tag"))
	sq.HighlightPostTag = unquoteJSON(opts.Get("highlight_post_tag"))
	sq.HighlightNumber = int(intOption("highlight_number", 0, opts))
	sq.HighlightSize = int(intOption("highlight_size", 0, opts))

	// JSON-decoded array parameters.
	if v := opts.Get("sort"); v != "" {
		var sort []string
		if json.Unmarshal([]byte(v), &sort) == nil {
			sq.Sort = sort
		}
	}
	if v := opts.Get("counts"); v != "" {
		var counts []string
		if json.Unmarshal([]byte(v), &counts) == nil {
			sq.Counts = counts
		}
	}
	if v := opts.Get("highlight_fields"); v != "" {
		var fields []string
		if json.Unmarshal([]byte(v), &fields) == nil {
			sq.HighlightFields = fields
		}
	}
	if v := opts.Get("include_fields"); v != "" {
		var fields []string
		if json.Unmarshal([]byte(v), &fields) == nil {
			sq.IncludeFields = fields
		}
	}
	if v := opts.Get("group_sort"); v != "" {
		var gsort []string
		if json.Unmarshal([]byte(v), &gsort) == nil {
			sq.GroupSort = gsort
		}
	}

	// Ranges: {"field": {"label": {"min": N, "max": N}, ...}, ...}
	if v := opts.Get("ranges"); v != "" {
		var raw map[string]map[string]struct {
			Min *float64 `json:"min"`
			Max *float64 `json:"max"`
		}
		if json.Unmarshal([]byte(v), &raw) == nil {
			sq.Ranges = make(map[string][]port.SearchRange)
			for field, ranges := range raw {
				for label, r := range ranges {
					sq.Ranges[field] = append(sq.Ranges[field], port.SearchRange{
						Label: label,
						Min:   r.Min,
						Max:   r.Max,
					})
				}
			}
		}
	}

	// Drilldown: [["field","val1","val2"], ...]
	// CouchDB also supports multiple drilldown=["field","val"] params.
	if v := opts.Get("drilldown"); v != "" {
		// Try as array of arrays first.
		var dd [][]string
		if json.Unmarshal([]byte(v), &dd) == nil {
			sq.Drilldown = dd
		} else {
			// Single drilldown entry: ["field","val1","val2"]
			var single []string
			if json.Unmarshal([]byte(v), &single) == nil {
				sq.Drilldown = [][]string{single}
			}
		}
		// Also collect additional drilldown params.
		for _, raw := range opts["drilldown"][1:] {
			var entry []string
			if json.Unmarshal([]byte(raw), &entry) == nil {
				sq.Drilldown = append(sq.Drilldown, entry)
			}
		}
	}

	return sq
}

// Response types matching CouchDB search API format.

type SearchResult struct {
	TotalRows uint64                    `json:"total_rows"`
	Bookmark  string                    `json:"bookmark"`
	Rows      []SearchRow               `json:"rows"`
	Counts    map[string]map[string]int `json:"counts,omitempty"`
	Ranges    map[string]map[string]int `json:"ranges,omitempty"`
}

type SearchRow struct {
	ID         string                 `json:"id"`
	Order      []float64              `json:"order"`
	Fields     map[string]interface{} `json:"fields"`
	Doc        map[string]interface{} `json:"doc,omitempty"`
	Highlights map[string][]string    `json:"highlights,omitempty"`
}

type SearchGroup struct {
	By        string      `json:"by"`
	TotalRows int         `json:"total_rows"`
	Rows      []SearchRow `json:"rows"`
	Bookmark  string      `json:"bookmark,omitempty"`
}

type SearchResultGrouped struct {
	TotalRows uint64        `json:"total_rows"`
	Bookmark  string        `json:"bookmark,omitempty"`
	Groups    []SearchGroup `json:"groups"`
}
