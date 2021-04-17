package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := mux.Vars(r)["docid"]
	if s.Design {
		docID = "_design/" + docID
	} else if s.Local {
		docID = "_local/" + docID
	}
	log.Println(docID)

	// options
	opts := r.URL.Query()
	revs := boolOption("revs", false, opts)
	localSeq := boolOption("local_seq", false, opts)
	// latest := boolOption("latest", false, opts)
	var openRevs []string
	if v := opts.Get("open_revs"); len(v) != 0 {
		err := json.Unmarshal([]byte(v), &openRevs)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	dbdoc, err := db.GetDocument(r.Context(), docID)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if localSeq {
		dbdoc.Data["_local_seq"] = dbdoc.LocalSeq
	}
	if revs {
		dbdoc.Data["_revisions"] = dbdoc.Revisions()
	}

	switch r.Header.Get("Accept") {
	case "multipart/mixed":
		mw := NewMultipartResponse(db, w)
		defer mw.Close()
		mw.WriteDocument(r.Context(), dbdoc)
	default:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbdoc.Data)
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
	json.NewEncoder(jw).Encode(doc.Data)

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

		r, err := r.db.AttachmentReader(doc.ID, attachement.Filename)
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
	r.mw.Close()
}
