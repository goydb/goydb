package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// DBUpdates handles GET /_db_updates.
// Returns a list of database events. For a single-node embedded server,
// this returns all current databases as "updated" events.
type DBUpdates struct {
	Base
}

func (s *DBUpdates) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	ctx := r.Context()
	names, err := s.Storage.Databases(ctx)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type dbEvent struct {
		DBName string `json:"db_name"`
		Type   string `json:"type"`
		Seq    string `json:"seq"`
	}

	results := make([]dbEvent, 0, len(names))
	for i, name := range names {
		results = append(results, dbEvent{
			DBName: name,
			Type:   "updated",
			Seq:    fmt.Sprintf("%d", i+1),
		})
	}

	lastSeq := "0"
	if len(results) > 0 {
		lastSeq = results[len(results)-1].Seq
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"results":  results,
		"last_seq": lastSeq,
	})
}
