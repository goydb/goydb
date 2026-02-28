package handler

import (
	"encoding/json"
	"net/http"
)

// DBShards handles GET /{db}/_shards.
// For a single-node server, returns one shard covering the full range.
type DBShards struct {
	Base
}

func (s *DBShards) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"shards": map[string][]string{
			"00000000-ffffffff": {localNode},
		},
	})
}

// DBShardsDoc handles GET /{db}/_shards/{docid}.
// Returns the shard range for a specific document.
type DBShardsDoc struct {
	Base
}

func (s *DBShardsDoc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"range": "00000000-ffffffff",
		"nodes": []string{localNode},
	})
}

// DBSyncShards handles POST /{db}/_sync_shards.
// No-op for single-node server.
type DBSyncShards struct {
	Base
}

func (s *DBSyncShards) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
}
