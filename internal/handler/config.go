package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// ConfigAll handles GET /_config — returns all sections.
type ConfigAll struct {
	Base
}

func (s *ConfigAll) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Config.All()) // nolint: errcheck
}

// ConfigSection handles GET /_config/{section}.
type ConfigSection struct {
	Base
}

func (s *ConfigSection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck
	section := mux.Vars(r)["section"]
	kv, ok := s.Config.Section(section)
	if !ok {
		WriteError(w, http.StatusNotFound, "unknown_config_value")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(kv) // nolint: errcheck
}

// ConfigKey handles GET /_config/{section}/{key}.
type ConfigKey struct {
	Base
}

func (s *ConfigKey) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck
	vars := mux.Vars(r)
	val, ok := s.Config.Get(vars["section"], vars["key"])
	if !ok {
		WriteError(w, http.StatusNotFound, "unknown_config_value")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(val) // nolint: errcheck
}

// ConfigKeyPut handles PUT /_config/{section}/{key}.
// The request body must be a JSON-encoded string; the old value is returned.
type ConfigKeyPut struct {
	Base
}

func (s *ConfigKeyPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck
	vars := mux.Vars(r)

	var value string
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	old := s.Config.Set(vars["section"], vars["key"], value)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(old) // nolint: errcheck
}

// ConfigReload handles POST /_node/{node}/_config/_reload.
// For an embedded server, config is always in-memory, so this is a no-op.
type ConfigReload struct {
	Base
}

func (s *ConfigReload) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
}

// ConfigKeyDelete handles DELETE /_config/{section}/{key}.
type ConfigKeyDelete struct {
	Base
}

func (s *ConfigKeyDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck
	vars := mux.Vars(r)
	old, ok := s.Config.Delete(vars["section"], vars["key"])
	if !ok {
		WriteError(w, http.StatusNotFound, "unknown_config_value")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(old) // nolint: errcheck
}
