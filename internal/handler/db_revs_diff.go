package handler

import (
	"encoding/json"
	"net/http"
)

type DBRevsDiff struct {
	Base
}

func (s *DBRevsDiff) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var req map[string][]string
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	type revsDiffEntry struct {
		Missing []string `json:"missing"`
	}

	result := make(map[string]*revsDiffEntry)

	for docID, revs := range req {
		doc, err := db.GetDocument(r.Context(), docID)
		if err != nil || doc == nil {
			// doc doesn't exist, all revisions are missing
			result[docID] = &revsDiffEntry{Missing: revs}
			continue
		}

		var missing []string
		for _, rev := range revs {
			if !doc.HasRevision(rev) {
				missing = append(missing, rev)
			}
		}
		if len(missing) > 0 {
			result[docID] = &revsDiffEntry{Missing: missing}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result) // nolint: errcheck
}
