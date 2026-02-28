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

// NodeInfo handles GET /_node/{node-name}.
// Returns the node name. The special name _local maps to the current node.
type NodeInfo struct {
	Base
}

func (s *NodeInfo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ // nolint: errcheck
		"name": localNode,
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
// For a single-node server, enable_single_node and finish_cluster are accepted.
// enable_cluster, add_node, and remove_node are accepted for API compatibility
// but are no-ops since this is always a single-node deployment.
type ClusterSetupPost struct {
	Base
}

func (s *ClusterSetupPost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch body.Action {
	case "enable_single_node", "finish_cluster", "enable_cluster", "add_node", "remove_node":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
	case "":
		WriteError(w, http.StatusBadRequest, "action is required")
	default:
		WriteError(w, http.StatusBadRequest, "unknown action: "+body.Action)
	}
}
