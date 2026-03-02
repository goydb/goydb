package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"github.com/mitchellh/mapstructure"
)

type DBDocPut struct {
	Base
}

func (s *DBDocPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	session, ok := Authenticator{Base: s.Base}.DB(w, r, db)
	if !ok {
		return
	}

	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/related" {
		s.handleMultipart(w, r, db, params["boundary"], session)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if CheckMaxDocumentSize(w, s.Config, int64(len(bodyBytes))) {
		return
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &doc); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	query := r.URL.Query()
	batch := query.Get("batch") == "ok"
	newEdits := query.Get("new_edits") != "false"

	docID := resolveDocID(doc, r)
	if docID == "" {
		WriteError(w, http.StatusBadRequest, "missing _id")
		return
	}

	// Extract inline base64 attachments before mapstructure touches _attachments.
	type inlineAttach struct {
		data        string
		contentType string
	}
	var inlineAttachments map[string]inlineAttach

	if rawAtts, ok := doc["_attachments"].(map[string]interface{}); ok {
		inlineAttachments = make(map[string]inlineAttach)
		for name, v := range rawAtts {
			if m, ok := v.(map[string]interface{}); ok {
				if data, ok := m["data"].(string); ok && data != "" {
					ct, _ := m["content_type"].(string)
					inlineAttachments[name] = inlineAttach{data: data, contentType: ct}
					delete(m, "data") // don't persist base64 in doc metadata
				}
			}
		}
	}

	var attachments map[string]*model.Attachment
	err = mapstructure.Decode(doc["_attachments"], &attachments)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check per-DB doc count for genuinely new documents (no _rev, newEdits=true, not _deleted).
	_, hasRev := doc["_rev"]
	if newEdits && !hasRev && !isTruthy(doc["_deleted"]) {
		if CheckMaxDocsPerDB(w, s.Config, r.Context(), db, 1) {
			return
		}
	}

	if CheckMaxDBSize(w, s.Config, r.Context(), db) {
		return
	}

	mdoc := &model.Document{
		ID:          docID,
		Data:        doc,
		Deleted:     isTruthy(doc["_deleted"]),
		Attachments: attachments,
	}

	// Run validate_doc_update for normal edits on non-local docs.
	if newEdits && !isLocalDoc(docID) {
		var oldDoc *model.Document
		if existing, err := db.GetDocument(r.Context(), docID); err == nil && existing != nil && !existing.Deleted {
			oldDoc = existing
		}
		if err := ValidateDocUpdate(r.Context(), db, s.Logger, mdoc, oldDoc, session); err != nil {
			if writeValidationError(w, err) {
				return
			}
		}
	}

	var rev string
	if !newEdits {
		// new_edits=false: store with the supplied rev (replication mode).
		if r, _ := doc["_rev"].(string); r != "" {
			mdoc.Rev = r
		}
		err = db.PutDocumentForReplication(r.Context(), mdoc)
		rev = mdoc.Rev
	} else {
		rev, err = db.PutDocument(r.Context(), mdoc)
	}
	if errors.Is(err, storage.ErrConflict) {
		WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Write inline base64 attachments after the document exists in storage.
	// Capture the rev from each PutAttachment call so we return the final rev.
	for name, att := range inlineAttachments {
		decoded, err := base64.StdEncoding.DecodeString(att.data)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad base64 in attachment "+name)
			return
		}
		if CheckMaxAttachmentSize(w, s.Config, int64(len(decoded))) {
			return
		}
		rev, err = db.PutAttachment(r.Context(), docID, &model.Attachment{
			Filename:    name,
			ContentType: att.contentType,
			Reader:      io.NopCloser(bytes.NewReader(decoded)),
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	status := http.StatusCreated
	if batch {
		status = http.StatusAccepted
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"ok":  true,
		"id":  docID,
		"rev": rev,
	})
}

func (s *DBDocPut) handleMultipart(w http.ResponseWriter, r *http.Request, db port.Database, boundary string, session *model.Session) {
	mr := multipart.NewReader(r.Body, boundary)

	// Part 1: JSON document.
	part, err := mr.NextPart()
	if err != nil {
		WriteError(w, http.StatusBadRequest, "multipart: missing JSON part: "+err.Error())
		return
	}
	defer part.Close() //nolint:errcheck

	partBytes, err := io.ReadAll(part)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if CheckMaxDocumentSize(w, s.Config, int64(len(partBytes))) {
		return
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(partBytes, &doc); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	docID := resolveDocID(doc, r)
	if docID == "" {
		WriteError(w, http.StatusBadRequest, "missing _id")
		return
	}

	if CheckMaxDBSize(w, s.Config, r.Context(), db) {
		return
	}

	mdoc := &model.Document{
		ID:      docID,
		Data:    doc,
		Deleted: isTruthy(doc["_deleted"]),
	}

	// Run validate_doc_update for non-local docs.
	if !isLocalDoc(docID) {
		var oldDoc *model.Document
		if existing, err := db.GetDocument(r.Context(), docID); err == nil && existing != nil && !existing.Deleted {
			oldDoc = existing
		}
		if err := ValidateDocUpdate(r.Context(), db, s.Logger, mdoc, oldDoc, session); err != nil {
			if writeValidationError(w, err) {
				return
			}
		}
	}

	rev, err := db.PutDocument(r.Context(), mdoc)
	if errors.Is(err, storage.ErrConflict) {
		WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parts 2+: binary attachment bodies.
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			WriteError(w, http.StatusBadRequest, "multipart: "+err.Error())
			return
		}

		filename := filenameFromPart(part)
		ct := part.Header.Get("Content-Type")

		attLimit := configInt64(s.Config, "couchdb", "max_attachment_size")
		reader := newLimitedReadCloser(io.NopCloser(part), attLimit)

		rev, err = db.PutAttachment(r.Context(), docID, &model.Attachment{
			Filename:    filename,
			ContentType: ct,
			Reader:      reader,
		})
		if errors.Is(err, ErrLimitExceeded) {
			WriteError(w, http.StatusRequestEntityTooLarge, "attachment_too_large")
			return
		}
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"ok":  true,
		"id":  docID,
		"rev": rev,
	})
}

// resolveDocID extracts the document ID from the URL path variable (authoritative)
// or falls back to the JSON body _id field.
func resolveDocID(doc map[string]interface{}, r *http.Request) string {
	if id, ok := mux.Vars(r)["docid"]; ok {
		if strings.Contains(r.URL.Path, "/_design/") {
			return string(model.DesignDocPrefix) + id
		} else if strings.Contains(r.URL.Path, "/_local/") {
			return "_local/" + id
		}
		return id
	}
	if id, ok := doc["_id"].(string); ok && id != "" {
		return id
	}
	return ""
}

// filenameFromPart parses Content-Disposition to extract the filename parameter.
func filenameFromPart(p *multipart.Part) string {
	_, params, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	return params["filename"]
}

// isTruthy returns true when v is the boolean true or the string "true".
func isTruthy(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	}
	return false
}
