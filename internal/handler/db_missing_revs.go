package handler

import (
	"encoding/json"
	"net/http"
)

type DBMissingRevs struct {
	Base
}

func (s *DBMissingRevs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var req map[string][]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	missingRevs := make(map[string][]string)

	for docID, revs := range req {
		doc, err := db.GetDocument(r.Context(), docID)
		if err != nil || doc == nil {
			// doc doesn't exist; all revisions are missing
			missingRevs[docID] = revs
			continue
		}

		var missing []string
		for _, rev := range revs {
			if !doc.HasRevision(rev) {
				missing = append(missing, rev)
			}
		}
		if len(missing) > 0 {
			missingRevs[docID] = missing
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"missing_revs": missingRevs,
	})
}
