package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// SearchCleanup handles POST /{db}/_search_cleanup and POST /{db}/_nouveau_cleanup.
// Returns ok; no-op for embedded server without full-text search indices.
type SearchCleanup struct {
	Base
}

func (s *SearchCleanup) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
}

// SearchAnalyze handles POST /_search_analyze and POST /_nouveau_analyze.
// Analyzes text using the specified analyzer. Returns the list of tokens.
type SearchAnalyze struct {
	Base
}

func (s *SearchAnalyze) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	var body struct {
		Analyzer string `json:"analyzer"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Simple whitespace tokenization as a basic analyzer
	tokens := []string{}
	if body.Text != "" {
		current := ""
		for _, ch := range body.Text {
			if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
				if current != "" {
					tokens = append(tokens, current)
					current = ""
				}
			} else {
				current += string(ch)
			}
		}
		if current != "" {
			tokens = append(tokens, current)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"tokens": tokens,
	})
}

// SearchInfo handles GET /{db}/_design/{ddoc}/_search_info/{index}.
// Returns information about the search index.
type SearchInfo struct {
	Base
}

func (s *SearchInfo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	vars := mux.Vars(r)
	ddocID := "_design/" + vars["docid"]
	indexName := vars["index"]

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"name": ddocID + "/" + indexName,
		"search_index": map[string]interface{}{
			"pending_seq":    0,
			"doc_del_count":  0,
			"doc_count":      0,
			"disk_size":      0,
			"committed_seq":  0,
			"compact_running": false,
		},
	})
}

// NouveauSearch handles POST /{db}/_design/{ddoc}/_nouveau/{index}.
// Nouveau full-text search endpoint. Returns empty results.
type NouveauSearch struct {
	Base
}

func (s *NouveauSearch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"total_rows": 0,
		"rows":       []interface{}{},
		"bookmark":   "nil",
	})
}

// NouveauInfo handles GET /{db}/_design/{ddoc}/_nouveau_info/{index}.
type NouveauInfo struct {
	Base
}

func (s *NouveauInfo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	vars := mux.Vars(r)
	ddocID := "_design/" + vars["docid"]
	indexName := vars["index"]

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"name": ddocID + "/" + indexName,
		"search_index": map[string]interface{}{
			"pending_seq":    0,
			"doc_del_count":  0,
			"doc_count":      0,
			"disk_size":      0,
			"committed_seq":  0,
			"compact_running": false,
		},
	})
}
