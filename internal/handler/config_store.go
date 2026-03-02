package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/goydb/goydb/pkg/port"
)

// defaultConfig is the seed applied on the very first run (when no file exists).
var defaultConfig = map[string]map[string]string{
	"couchdb": {
		"uuid":                "0dbc95c8-4208-11eb-ad76-00155d4c9c92",
		"version":             "0.1.0",
		"max_document_size":   "8000000",
		"max_dbs":             "0",
		"max_docs_per_db":     "0",
		"max_attachment_size":        "0",
		"max_db_size":               "0",
		"validate_on_replication":   "false",
	},
	"chttpd": {
		"max_http_request_size": "4294967296",
		"admin_only_all_dbs":    "true",
	},
	"httpd": {
		"bind_address":            "0.0.0.0",
		"port":                    "7070",
		"enable_cors":             "false",
		"authentication_handlers": "cookie_authentication_handler,default_authentication_handler",
	},
	"cors": {
		"origins":     "*",
		"credentials": "false",
		"headers":     "accept, authorization, content-type, origin, referer",
		"methods":     "GET, PUT, POST, HEAD, DELETE",
	},
	"log": {
		"level": "info",
		"file":  "",
	},
	"admins": {},
}

// ConfigStore is a thread-safe CouchDB-style configuration store.
// When path is non-empty the store is backed by a JSON file and every write
// is atomically persisted so values survive a server restart.
type ConfigStore struct {
	mu     sync.RWMutex
	path   string                      // empty → in-memory only
	data   map[string]map[string]string // section → key → value
	logger port.Logger                 // optional logger, can be nil during bootstrap
}

// NewConfigStore returns a ConfigStore for the given file path.
//   - path == "" → pure in-memory, seeded with defaults (useful for tests)
//   - path != "" and file exists → loaded from file (file is authoritative)
//   - path != "" and file absent → seeded with defaults, file created immediately
//   - logger can be nil during bootstrap, in which case errors are written to stderr
func NewConfigStore(path string, logger port.Logger) *ConfigStore {
	cs := &ConfigStore{path: path, logger: logger}

	if path != "" {
		if data, err := loadConfigFile(path); err == nil {
			cs.data = data
			return cs
		}
		// File absent or unreadable — seed defaults and persist them.
	}

	cs.data = deepCopyDefaults()
	if path != "" {
		if err := cs.saveUnlocked(); err != nil {
			if cs.logger != nil {
				cs.logger.Warnf(context.Background(), "config file init failed", "path", path, "error", err)
			} else {
				fmt.Fprintf(os.Stderr, "WARNING: config: failed to write initial config file %q: %v\n", path, err)
			}
		}
	}
	return cs
}

// All returns a deep copy of all sections.
func (cs *ConfigStore) All() map[string]map[string]string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return deepCopy(cs.data)
}

// Section returns a copy of one section, and whether it exists.
func (cs *ConfigStore) Section(section string) (map[string]string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	kv, ok := cs.data[section]
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(kv))
	for k, v := range kv {
		out[k] = v
	}
	return out, true
}

// Get returns a single value and whether it exists.
func (cs *ConfigStore) Get(section, key string) (string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	kv, ok := cs.data[section]
	if !ok {
		return "", false
	}
	v, ok := kv[key]
	return v, ok
}

// Set stores a value and returns the previous value (empty string if absent).
func (cs *ConfigStore) Set(section, key, value string) string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.data[section] == nil {
		cs.data[section] = map[string]string{}
	}
	old := cs.data[section][key]
	cs.data[section][key] = value
	if cs.path != "" {
		if err := cs.saveUnlocked(); err != nil {
			if cs.logger != nil {
				cs.logger.Warnf(context.Background(), "config save failed", "error", err)
			} else {
				fmt.Fprintf(os.Stderr, "WARNING: config: failed to save config: %v\n", err)
			}
		}
	}
	return old
}

// Delete removes a key and returns (old value, true) or ("", false) if absent.
func (cs *ConfigStore) Delete(section, key string) (string, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	kv, ok := cs.data[section]
	if !ok {
		return "", false
	}
	old, ok := kv[key]
	if !ok {
		return "", false
	}
	delete(kv, key)
	if cs.path != "" {
		if err := cs.saveUnlocked(); err != nil {
			if cs.logger != nil {
				cs.logger.Warnf(context.Background(), "config save failed", "error", err)
			} else {
				fmt.Fprintf(os.Stderr, "WARNING: config: failed to save config: %v\n", err)
			}
		}
	}
	return old, true
}

// saveUnlocked writes cs.data to cs.path atomically (temp file + rename).
// Must be called with cs.mu held for writing.
func (cs *ConfigStore) saveUnlocked() error {
	data, err := json.MarshalIndent(cs.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := cs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, cs.path)
}

// loadConfigFile reads and unmarshals the JSON config file at path.
func loadConfigFile(path string) (map[string]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func deepCopyDefaults() map[string]map[string]string {
	return deepCopy(defaultConfig)
}

func deepCopy(src map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(src))
	for section, kv := range src {
		sec := make(map[string]string, len(kv))
		for k, v := range kv {
			sec[k] = v
		}
		out[section] = sec
	}
	return out
}
