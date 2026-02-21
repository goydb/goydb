package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBDocGet struct {
	Base
	Design bool
	Local  bool
}

func (s *DBDocGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := mux.Vars(r)["docid"]
	if s.Design {
		docID = string(model.DesignDocPrefix) + docID
	} else if s.Local {
		docID = string(model.LocalDocPrefix) + docID
	}

	// options
	opts := r.URL.Query()
	revs := boolOption("revs", false, opts)
	localSeq := boolOption("local_seq", false, opts)
	// latest := boolOption("latest", false, opts)
	var openRevs []string
	if v := opts.Get("open_revs"); len(v) != 0 {
		if v == "all" {
			openRevs = []string{"all"}
		} else {
			if err := json.Unmarshal([]byte(v), &openRevs); err != nil {
				WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	// Handle open_revs — return JSON array of leaf revisions.
	if len(openRevs) > 0 {
		type openRevEntry struct {
			OK      *model.Document `json:"ok,omitempty"`
			Missing string          `json:"missing,omitempty"`
		}

		var leaves []*model.Document
		var fetchErr error
		if openRevs[0] == "all" {
			leaves, fetchErr = db.GetLeaves(r.Context(), docID)
		} else {
			for _, rev := range openRevs {
				leaf, e := db.GetLeaf(r.Context(), docID, rev)
				if e == nil && leaf != nil {
					leaves = append(leaves, leaf)
				}
			}
		}
		if fetchErr != nil {
			WriteError(w, http.StatusInternalServerError, fetchErr.Error())
			return
		}

		var result []openRevEntry
		if openRevs[0] == "all" {
			for _, l := range leaves {
				result = append(result, openRevEntry{OK: l})
			}
		} else {
			found := make(map[string]*model.Document, len(leaves))
			for _, l := range leaves {
				found[l.Rev] = l
			}
			for _, rev := range openRevs {
				if l, ok := found[rev]; ok {
					result = append(result, openRevEntry{OK: l})
				} else {
					result = append(result, openRevEntry{Missing: rev})
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result) //nolint:errcheck
		return
	}

	dbdoc, err := db.GetDocument(r.Context(), docID)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if dbdoc == nil {
		WriteError(w, http.StatusNotFound, "document not found")
		return
	}

	// Always create a clean response map without modifying dbdoc.Data
	// This prevents fields like _revisions from persisting in the document
	responseData := make(map[string]interface{}, len(dbdoc.Data)+3)
	for k, v := range dbdoc.Data {
		// Filter out internal fields that should only be added conditionally
		if k == "_revisions" || k == "_local_seq" {
			continue
		}
		responseData[k] = v
	}

	// Add conditional fields
	if localSeq {
		responseData["_local_seq"] = dbdoc.LocalSeq
	}
	if revs {
		responseData["_revisions"] = dbdoc.Revisions()
	}

	switch r.Header.Get("Accept") {
	case "multipart/mixed":
		// For multipart, use a temporary document with clean data
		tempDoc := *dbdoc
		tempDoc.Data = responseData
		mw := NewMultipartResponse(db, w)
		defer mw.Close()
		err = mw.WriteDocument(r.Context(), &tempDoc)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responseData) // nolint: errcheck
	}
}

type MultipartResponse struct {
	db port.Database
	mw *multipart.Writer
}

func NewMultipartResponse(db port.Database, w http.ResponseWriter) *MultipartResponse {
	// root writer
	mw := multipart.NewWriter(w)
	w.Header().Set("Content-Type", fmt.Sprintf(`multipart/mixed; boundary="%s"`, mw.Boundary()))

	return &MultipartResponse{
		db: db,
		mw: mw,
	}
}

func (r *MultipartResponse) WriteDocument(ctx context.Context, doc *model.Document) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// json header
	jw, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		return err
	}
	err = json.NewEncoder(jw).Encode(doc.Data)
	if err != nil {
		return err
	}

	// attachments
	for _, attachement := range doc.Attachments {
		// allow stop by user
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		aw, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Type": []string{attachement.ContentType},
			"Content-Disposition": []string{
				fmt.Sprintf(`attachment; filename="%s"`, attachement.Filename),
			},
			"Content-Length": []string{
				strconv.FormatInt(attachement.Length, 10),
			},
		})
		if err != nil {
			return err
		}

		r, err := r.db.AttachmentReader(attachement.Digest)
		if err != nil {
			return err
		}

		_, err = io.Copy(aw, r)
		if err != nil {
			return err
		}
	}
	err = w.Close()
	if err != nil {
		return err
	}

	// per document writer
	p, err := r.mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{fmt.Sprintf(`multipart/related; boundary="%s"`, w.Boundary())},
	})
	if err != nil {
		return err
	}
	// remove last \r\n
	data := buf.Bytes()
	data = data[:len(data)-1]
	_, err = p.Write(data)
	return err
}

func (r *MultipartResponse) Close() {
	_ = r.mw.Close()
}
