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

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/related" {
		s.handleMultipart(w, r, db, params["boundary"])
		return
	}

	var doc map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&doc)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

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

	rev, err := db.PutDocument(r.Context(), &model.Document{
		ID:          docID,
		Data:        doc,
		Deleted:     doc["_deleted"] == "true",
		Attachments: attachments,
	})
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"ok":  true,
		"id":  docID,
		"rev": rev,
	})
}

func (s *DBDocPut) handleMultipart(w http.ResponseWriter, r *http.Request, db port.Database, boundary string) {
	mr := multipart.NewReader(r.Body, boundary)

	// Part 1: JSON document.
	part, err := mr.NextPart()
	if err != nil {
		WriteError(w, http.StatusBadRequest, "multipart: missing JSON part: "+err.Error())
		return
	}
	defer part.Close() //nolint:errcheck

	var doc map[string]interface{}
	if err := json.NewDecoder(part).Decode(&doc); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	docID := resolveDocID(doc, r)
	if docID == "" {
		WriteError(w, http.StatusBadRequest, "missing _id")
		return
	}

	rev, err := db.PutDocument(r.Context(), &model.Document{
		ID:      docID,
		Data:    doc,
		Deleted: doc["_deleted"] == "true",
	})
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

		rev, err = db.PutAttachment(r.Context(), docID, &model.Attachment{
			Filename:    filename,
			ContentType: ct,
			Reader:      io.NopCloser(part),
		})
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

// resolveDocID extracts the document ID from the JSON body or the URL path variable.
func resolveDocID(doc map[string]interface{}, r *http.Request) string {
	if id, ok := doc["_id"].(string); ok && id != "" {
		return id
	}
	if id, ok := mux.Vars(r)["docid"]; ok {
		if strings.Contains(r.URL.Path, "/_design/") {
			return string(model.DesignDocPrefix) + id
		} else if strings.Contains(r.URL.Path, "/_local/") {
			return "_local/" + id
		}
		return id
	}
	return ""
}

// filenameFromPart parses Content-Disposition to extract the filename parameter.
func filenameFromPart(p *multipart.Part) string {
	_, params, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	return params["filename"]
}
