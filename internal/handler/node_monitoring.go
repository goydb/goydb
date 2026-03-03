package handler

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// NodeStats handles GET /_node/{node}/_stats.
type NodeStats struct {
	Base
}

func (s *NodeStats) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"couchdb": map[string]interface{}{
			"httpd_request_methods": map[string]interface{}{
				"GET":    map[string]interface{}{"value": 0, "type": "counter", "desc": "number of HTTP GET requests"},
				"POST":   map[string]interface{}{"value": 0, "type": "counter", "desc": "number of HTTP POST requests"},
				"PUT":    map[string]interface{}{"value": 0, "type": "counter", "desc": "number of HTTP PUT requests"},
				"DELETE": map[string]interface{}{"value": 0, "type": "counter", "desc": "number of HTTP DELETE requests"},
			},
			"httpd_status_codes": map[string]interface{}{
				"200": map[string]interface{}{"value": 0, "type": "counter", "desc": "number of HTTP 200 OK responses"},
				"201": map[string]interface{}{"value": 0, "type": "counter", "desc": "number of HTTP 201 Created responses"},
				"404": map[string]interface{}{"value": 0, "type": "counter", "desc": "number of HTTP 404 Not Found responses"},
			},
		},
	})
}

// NodeSystem handles GET /_node/{node}/_system.
type NodeSystem struct {
	Base
}

func (s *NodeSystem) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"memory": map[string]interface{}{
			"other":     memStats.Sys - memStats.HeapSys,
			"atom":      0,
			"binary":    memStats.HeapInuse,
			"processes": memStats.HeapAlloc,
		},
		"run_queue":              runtime.NumGoroutine(),
		"message_queues":         0,
		"internal_replication_jobs": 0,
	})
}

// NodeRestart handles POST /_node/{node}/_restart.
// Returns ok but does not actually restart (single-process embedded server).
type NodeRestart struct {
	Base
}

func (s *NodeRestart) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
}

// NodeVersions handles GET /_node/{node}/_versions.
type NodeVersions struct {
	Base
}

func (s *NodeVersions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	versions := map[string]interface{}{
		"collation_driver": map[string]interface{}{
			"name":    "builtin",
			"version": "0.0.0",
		},
	}
	if jsEngineInfo != nil {
		versions["javascript_engine"] = jsEngineInfo
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions) // nolint: errcheck
}

// NodeSmooshStatus handles GET /_node/{node}/_smoosh/status.
type NodeSmooshStatus struct {
	Base
}

func (s *NodeSmooshStatus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"channels": map[string]interface{}{},
	})
}
