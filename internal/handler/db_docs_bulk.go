package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBDocsBulk struct {
	Base
	Design bool
}

func (s *DBDocsBulk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	session, ok := Authenticator{Base: s.Base}.DB(w, r, db)
	if !ok {
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check total request body against max_http_request_size.
	httpLimit := configInt64(s.Config, "chttpd", "max_http_request_size")
	if httpLimit > 0 && int64(len(bodyBytes)) > httpLimit {
		WriteError(w, http.StatusRequestEntityTooLarge, "request_entity_too_large")
		return
	}

	var req BulkDocRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check each document's size against max_document_size.
	docSizeLimit := configInt64(s.Config, "couchdb", "max_document_size")
	if docSizeLimit > 0 {
		for _, doc := range req.Docs {
			docBytes, err := json.Marshal(doc.Data)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if int64(len(docBytes)) > docSizeLimit {
				WriteError(w, http.StatusRequestEntityTooLarge, "document_too_large")
				return
			}
		}
	}

	newEdits := req.NewEdits == nil || *req.NewEdits

	// Count new docs in the batch (no Rev, not Deleted) and check max_docs_per_db.
	// Skip when new_edits=false (replication mode).
	if newEdits {
		var newCount int64
		for _, doc := range req.Docs {
			if doc.Rev == "" && !doc.Deleted {
				newCount++
			}
		}
		if newCount > 0 {
			if CheckMaxDocsPerDB(w, s.Config, r.Context(), db, newCount) {
				return
			}
		}
	}

	if CheckMaxDBSize(w, s.Config, r.Context(), db) {
		return
	}

	// Pre-validate all docs against VDU functions (when newEdits=true).
	vduSkip := make(map[int]bool)
	resp := make([]SimpleDocResponse, len(req.Docs))
	if newEdits {
		for i, doc := range req.Docs {
			if isLocalDoc(doc.ID) {
				continue
			}
			var oldDoc *model.Document
			if doc.Rev != "" || doc.Deleted {
				if existing, err := db.GetDocument(r.Context(), doc.ID); err == nil && existing != nil && !existing.Deleted {
					oldDoc = existing
				}
			}
			if err := ValidateDocUpdate(r.Context(), db, s.Logger, doc, oldDoc, session); err != nil {
				resp[i].ID = doc.ID
				resp[i].Ok = false
				var forbiddenErr *model.ErrForbidden
				var unauthorizedErr *model.ErrUnauthorized
				if errors.As(err, &forbiddenErr) {
					resp[i].Error, resp[i].Reason = "forbidden", forbiddenErr.Msg
				} else if errors.As(err, &unauthorizedErr) {
					resp[i].Error, resp[i].Reason = "unauthorized", unauthorizedErr.Msg
				} else {
					resp[i].Error, resp[i].Reason = "internal_server_error", err.Error()
				}
				vduSkip[i] = true
			}
		}
	}

	err = db.Transaction(r.Context(), func(tx port.DatabaseTx) error {
		for i, doc := range req.Docs {
			if vduSkip[i] {
				continue
			}
			var rev string
			var err error

			if !newEdits {
				err = tx.PutDocumentForReplication(r.Context(), doc)
				rev = doc.Rev
			} else if doc.Deleted {
				doc, err2 := tx.DeleteDocument(r.Context(), doc.ID, doc.Rev)
				rev, err = doc.Rev, err2
			} else {
				rev, err = tx.PutDocument(r.Context(), doc)
			}

			resp[i].ID = doc.ID
			if err != nil {
				resp[i].Ok = false
				switch {
				case errors.Is(err, port.ErrConflict):
					resp[i].Error, resp[i].Reason = "conflict", "Document update conflict."
				case errors.Is(err, port.ErrNotFound):
					resp[i].Error, resp[i].Reason = "not_found", "missing"
				default:
					resp[i].Error, resp[i].Reason = "internal_server_error", err.Error()
				}
				s.Logger.Warnf(r.Context(), "failed to put document in bulk", "docID", doc.ID, "error", err)
			} else {
				resp[i].Ok = true
				resp[i].Rev = rev
			}
		}
		return nil
	})
	if err != nil {
		s.Logger.Errorf(r.Context(), "bulk docs transaction failed", "error", err)
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Notify change listeners for each successfully stored document.
	// This must happen after the transaction commits so the changes are
	// visible to listeners that re-read from storage.
	for i, doc := range req.Docs {
		if resp[i].Ok {
			db.NotifyDocumentUpdate(doc)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // nolint: errcheck
}

type BulkDocRequest struct {
	Docs     []*model.Document `json:"docs"`
	NewEdits *bool             `json:"new_edits,omitempty"`
}
