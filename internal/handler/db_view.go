package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// viewKeyRange parses startkey / endkey / key / inclusive_end from URL query
// params and returns CBOR-encoded byte slices ready for SetStartKey /
// SetEndKey on a view iterator.
//
// View bucket keys have the format: CBOR(emittedKey) + seq(8 B) + keyLen(2 B).
// For an inclusive end-key, we append 10 × 0xFF so that any real seq/keyLen
// bytes still compare ≤ the padded sentinel.  For an exclusive end-key the
// bare CBOR bytes are sufficient: the extra seq+keyLen bytes always push the
// real bucket key past the bare CBOR prefix, so Continue(cmp < 0) correctly
// stops before rows whose emitted key equals the endkey.
func viewKeyRange(options interface{ Get(string) string }) (startKey, endKey []byte, decodedStart, decodedEnd interface{}, exclusiveEnd bool) {
	jsonToViewKey := func(raw string) ([]byte, interface{}) {
		if raw == "" {
			return nil, nil
		}
		var v interface{}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, nil
		}
		b, err := cbor.Marshal(v)
		if err != nil {
			return nil, nil
		}
		return b, v
	}

	var dsk, dek interface{}
	sk, dsk := jsonToViewKey(options.Get("startkey"))
	if sk == nil {
		sk, dsk = jsonToViewKey(options.Get("start_key"))
	}

	ek, dek := jsonToViewKey(options.Get("endkey"))
	if ek == nil {
		ek, dek = jsonToViewKey(options.Get("end_key"))
	}

	// key=X is shorthand for startkey=X&endkey=X (inclusive)
	if k, dk := jsonToViewKey(options.Get("key")); k != nil {
		sk = k
		ek = k
		dsk = dk
		dek = dk
	}

	inclusive := true
	if ie := options.Get("inclusive_end"); ie == "false" {
		inclusive = false
		exclusiveEnd = true
	}

	// Pad inclusive end-key with 10 × 0xFF to cover the seq+keyLen suffix of
	// any real bucket key whose emitted-key CBOR bytes equal the endkey CBOR.
	if ek != nil && inclusive {
		ek = append(ek, bytes.Repeat([]byte{0xFF}, 10)...)
	}

	return sk, ek, dsk, dek, exclusiveEnd
}

type DBView struct {
	Base
}

func (s *DBView) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := string(model.DesignDocPrefix) + mux.Vars(r)["docid"]
	viewName := mux.Vars(r)["view"]

	ddfn := model.DesignDocFn{
		Type:        model.ViewFn,
		DesignDocID: docID,
		FnName:      viewName,
	}
	idx, ok := db.Indices()[ddfn.String()]
	if !ok {
		WriteError(w, http.StatusNotFound, "index not found")
		return
	}

	options := r.URL.Query()
	var update string
	if len(options["update"]) > 0 {
		update = options["update"][0]
	}

	switch update {
	case "", "true":
		// wait for all view updates to take place
		for {
			n, err := db.TaskCount(r.Context())
			if err != nil {
				s.Logger.Errorf(r.Context(), "failed to get task count", "error", err)
				WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if n == 0 {
				break
			}
			time.Sleep(time.Second)
		}
	/*case "lazy":
	err = db.AddTasks(r.Context(), []*model.Task{
		&model.Task{
			Action:    model.ActionUpdateView,
			DBName:    db.Name(),
			ViewDocID: docID,
		},
	})*/
	case "false": // do nothing
	}

	var q port.AllDocsQuery
	q.Skip = intOption("skip", 0, options)
	q.Limit = intOption("limit", 100, options)
	q.DDFN = &model.DesignDocFn{
		Type:        model.ViewFn,
		DesignDocID: docID,
		FnName:      viewName,
	}
	q.IncludeDocs = boolOption("include_docs", false, options)
	q.ViewGroup = stringOption("group", "", options)
	q.ViewStartKey, q.ViewEndKey, q.ViewDecodedStartKey, q.ViewDecodedEndKey, q.ViewExclusiveEnd = viewKeyRange(options)

	var total int
	var err error

	if boolOption("reduce", true, options) {
		var docs map[interface{}]interface{}
		err = db.Transaction(r.Context(), func(tx port.DatabaseTx) error {
			designDoc, err := tx.GetDocument(r.Context(), docID)
			if err != nil {
				return err
			}

			view, ok := designDoc.View(ddfn.FnName)
			if ok {
				docs, total, err = controller.DesignDoc{
					DB: db,
				}.ReduceDocs(r.Context(), tx, idx, q, view)
			} else {
				err = fmt.Errorf("unknown view function name: %q", ddfn.FnName)
			}

			return err
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		response := AllDocsResponse{
			TotalRows: total,
			Rows:      make([]Rows, len(docs)),
		}
		i := 0
		for key, value := range docs {
			if doc, ok := value.(*model.Document); ok {
				response.Rows[i].ID = doc.ID
				response.Rows[i].Key = doc.Key
				response.Rows[i].Value = doc.Value
			} else {
				response.Rows[i].Key = key
				response.Rows[i].Value = value
			}
			i++
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response) // nolint: errcheck
	} else {
		var docList []*model.Document
		err = db.Transaction(r.Context(), func(tx port.DatabaseTx) error {
			iter, err := db.IndexIterator(r.Context(), tx, idx)
			if err != nil {
				return err
			}

			iter.SetSkip(int(q.Skip))
			iter.SetLimit(int(q.Limit))
			if q.ViewStartKey != nil {
				iter.SetStartKey(q.ViewStartKey)
			}
			if q.ViewEndKey != nil {
				iter.SetEndKey(q.ViewEndKey)
				iter.SetExclusiveEnd(q.ViewExclusiveEnd)
			}
			for doc := iter.First(); iter.Continue(); doc = iter.Next() {
				// Semantic post-filter: CBOR byte ordering doesn't match CouchDB
				// collation order (length-prefixed strings sort by length first).
				// Use ViewKeyCmp to correctly include/exclude rows.
				if q.ViewDecodedStartKey != nil && model.ViewKeyCmp(doc.Key, q.ViewDecodedStartKey) < 0 {
					continue
				}
				if q.ViewDecodedEndKey != nil {
					cmp := model.ViewKeyCmp(doc.Key, q.ViewDecodedEndKey)
					if q.ViewExclusiveEnd && cmp >= 0 {
						continue
					} else if !q.ViewExclusiveEnd && cmp > 0 {
						continue
					}
				}
				docList = append(docList, doc)
			}
			total = iter.Remaining() + len(docList)

			return err
		})

		// Enrich documents with full data if include_docs=true
		if err == nil && q.IncludeDocs && len(docList) > 0 {
			err = db.EnrichDocuments(r.Context(), docList)
		}
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		response := AllDocsResponse{
			TotalRows: total,
			Rows:      make([]Rows, len(docList)),
		}
		for i, doc := range docList {
			response.Rows[i].ID = doc.ID
			response.Rows[i].Key = doc.Key
			response.Rows[i].Value = doc.Value
			if q.IncludeDocs && doc.Data != nil {
				response.Rows[i].Doc = doc.Data
				response.Rows[i].Doc["_id"] = doc.ID
				response.Rows[i].Doc["_rev"] = doc.Rev
				if doc.Deleted {
					response.Rows[i].Doc["_deleted"] = doc.Deleted
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response) // nolint: errcheck
	}
}
