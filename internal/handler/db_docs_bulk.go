package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBDocsBulk struct {
	Base
	Design bool
}

func (s *DBDocsBulk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var req BulkDocRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := make([]SimpleDocResponse, len(req.Docs))
	err = db.Transaction(r.Context(), func(tx port.Transaction) error {
		for i, doc := range req.Docs {
			var rev string
			var err error

			if doc.Deleted {
				doc, err2 := tx.DeleteDocument(r.Context(), doc.ID, doc.Rev)
				rev, err = doc.Rev, err2
			} else {
				rev, err = tx.PutDocument(r.Context(), doc)
			}

			resp[i].ID = doc.ID
			if err != nil {
				resp[i].Ok = false
				log.Println(err)
			} else {
				resp[i].Ok = true
				resp[i].Rev = rev
			}
		}
		return nil
	})
	if err != nil {
		log.Println(err)
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type BulkDocRequest struct {
	Docs []*model.Document `json:"docs"`
}
