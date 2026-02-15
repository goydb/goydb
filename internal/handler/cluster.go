package handler

import (
	"encoding/json"
	"net/http"
)

const localNode = "nonode@nohost"

// Membership handles GET /_membership.
// For a single-node server the current node is always the sole member.
type Membership struct {
	Base
}

func (s *Membership) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"all_nodes":     []string{localNode},
		"cluster_nodes": []string{localNode},
	})
}

// ClusterSetupGet handles GET /_cluster_setup.
type ClusterSetupGet struct {
	Base
}

func (s *ClusterSetupGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ // nolint: errcheck
		"state": "single_node_enabled",
	})
}

// ClusterSetupPost handles POST /_cluster_setup.
// For a single-node server all setup actions are no-ops.
type ClusterSetupPost struct {
	Base
}

func (s *ClusterSetupPost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
}
