package handler

import (
	"encoding/json"
	"net/http"
)

// ReshardGet handles GET /_reshard.
// Returns resharding status. Single-node server has no resharding.
type ReshardGet struct {
	Base
}

func (s *ReshardGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"state":            "running",
		"state_reason":     nil,
		"completed":        0,
		"failed":           0,
		"running":          0,
		"stopped":          0,
		"total":            0,
	})
}

// ReshardState handles GET/PUT /_reshard/state.
type ReshardState struct {
	Base
}

func (s *ReshardState) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if r.Method == "PUT" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"state":        "running",
		"state_reason": nil,
	})
}

// ReshardJobs handles GET/POST /_reshard/jobs.
type ReshardJobs struct {
	Base
}

func (s *ReshardJobs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if r.Method == "POST" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{}) // nolint: errcheck
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"jobs":       []interface{}{},
		"offset":     0,
		"total_rows": 0,
	})
}
