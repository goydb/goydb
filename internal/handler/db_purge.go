package handler

import (
	"encoding/json"
	"net/http"
)

// DBPurge handles POST /{db}/_purge.
// Accepts a JSON object mapping document IDs to arrays of revision IDs.
// Returns the purge sequence and the purged revisions.
// Note: In this implementation, purge is accepted for API compatibility
// but does not remove tombstones from storage.
type DBPurge struct {
	Base
}

func (s *DBPurge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var body map[string][]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build response echoing back what was requested as "purged"
	purged := make(map[string][]string, len(body))
	for docID, revs := range body {
		purged[docID] = revs
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"purge_seq": nil,
		"purged":    purged,
	})
}

// DBPurgedInfosLimitGet handles GET /{db}/_purged_infos_limit.
type DBPurgedInfosLimitGet struct {
	Base
}

func (s *DBPurgedInfosLimitGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(1000) // nolint: errcheck
}

// DBPurgedInfosLimitPut handles PUT /{db}/_purged_infos_limit.
type DBPurgedInfosLimitPut struct {
	Base
}

func (s *DBPurgedInfosLimitPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint: errcheck
}
