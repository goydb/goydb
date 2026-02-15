package handler

import (
	"encoding/json"
	"net/http"
)

type DBDesignDocs struct {
	Base
}

func (s *DBDesignDocs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docs, total, err := db.AllDesignDocs(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := AllDocsResponse{
		TotalRows: total,
		Rows:      make([]Rows, len(docs)),
	}

	for i, doc := range docs {
		response.Rows[i].ID = doc.ID
		response.Rows[i].Key = doc.ID
		response.Rows[i].Value = Value{Rev: doc.Rev}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response) // nolint: errcheck
}
