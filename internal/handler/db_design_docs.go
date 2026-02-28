package handler

import (
	"encoding/json"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

type DBDesignDocs struct {
	Base
}

func (s *DBDesignDocs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	options := r.URL.Query()
	includeDocs := boolOption("include_docs", false, options)

	// Handle POST with keys body.
	if r.Method == "POST" {
		var body struct {
			Keys []string `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && len(body.Keys) > 0 {
			response := AllDocsResponse{
				TotalRows: len(body.Keys),
				Rows:      make([]Rows, len(body.Keys)),
			}
			for i, key := range body.Keys {
				docID := key
				if len(docID) > len(model.DesignDocPrefix) && docID[:len(model.DesignDocPrefix)] != string(model.DesignDocPrefix) {
					docID = string(model.DesignDocPrefix) + docID
				}
				doc, err := db.GetDocument(r.Context(), docID)
				if err != nil || doc == nil {
					response.Rows[i] = Rows{Key: key, Error: "not_found"}
					continue
				}
				response.Rows[i].ID = doc.ID
				response.Rows[i].Key = doc.ID
				response.Rows[i].Value = Value{Rev: doc.Rev}
				if includeDocs {
					response.Rows[i].Doc = doc.Data
					if response.Rows[i].Doc == nil {
						response.Rows[i].Doc = make(map[string]interface{})
					}
					response.Rows[i].Doc["_id"] = doc.ID
					response.Rows[i].Doc["_rev"] = doc.Rev
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response) // nolint: errcheck
			return
		}
	}

	docs, total, err := db.AllDesignDocs(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := AllDocsResponse{
		TotalRows: total,
		Rows:      formatDocRows(docs, includeDocs),
	}

	if boolOption("update_seq", false, options) {
		seq, _ := db.Sequence(r.Context())
		response.UpdateSeq = json.RawMessage(`"` + seq + `"`)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response) // nolint: errcheck
}
