package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// ViewResponse is the JSON response for view queries.  total_rows, offset and
// update_seq are omitted when nil (sorted=false or update_seq not requested).
type ViewResponse struct {
	TotalRows *int        `json:"total_rows,omitempty"`
	Offset    *int        `json:"offset,omitempty"`
	UpdateSeq interface{} `json:"update_seq,omitempty"`
	Rows      []Rows      `json:"rows"`
}

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

// cborKeyRange returns CBOR start/end keys for exact multi-key lookup.
// startKey = bare CBOR(v), endKey = CBOR(v) + 10×0xFF (inclusive bucket range).
func cborKeyRange(v interface{}) (startKey, endKey []byte) {
	b, err := cbor.Marshal(v)
	if err != nil {
		return nil, nil
	}
	startKey = b
	endKey = append(append([]byte{}, b...), bytes.Repeat([]byte{0xFF}, 10)...)
	return
}

// mergeBodyIntoOptions parses the JSON request body and merges fields into
// opts as url.Values.  Existing URL query parameters always take precedence.
//
// Each body field's raw JSON bytes are set as the option value so that
// downstream parsers (viewKeyRange, boolOption, intOption, etc.) receive the
// same format they would get from a URL query parameter:
//   - JSON string  "bob"      → opts["startkey"] = `"bob"` → json.Unmarshal works
//   - JSON bool    true       → opts["reduce"]   = "true"  → boolOption works
//   - JSON integer 2          → opts["limit"]    = "2"     → intOption works
//   - JSON array   ["a","b"]  → opts["keys"]     = `["a","b"]` → json.Unmarshal works
func mergeBodyIntoOptions(r *http.Request, opts url.Values) url.Values {
	if r.Method != "POST" {
		return opts
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return opts
	}
	for k, raw := range body {
		if len(opts[k]) > 0 {
			continue // URL param wins
		}
		opts.Set(k, string(raw))
	}
	return opts
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

	// Merge URL query params with JSON body (URL params take precedence).
	options := mergeBodyIntoOptions(r, r.URL.Query())

	var update string
	if len(options["update"]) > 0 {
		update = options["update"][0]
	}

	// stale=ok or stale=update_after → skip the view update wait.
	if stale := options.Get("stale"); stale == "ok" || stale == "update_after" {
		update = "false"
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
	q.ViewGroupLevel = int(intOption("group_level", 0, options))
	q.ViewDescending = boolOption("descending", false, options)
	q.ViewUpdateSeq = boolOption("update_seq", false, options)
	// sorted defaults to true; only false when explicitly "false"
	q.ViewOmitSortedInfo = options.Get("sorted") == "false"

	// Parse multi-key lookup.
	if keysRaw := options.Get("keys"); keysRaw != "" {
		var keys []interface{}
		if err := json.Unmarshal([]byte(keysRaw), &keys); err == nil {
			q.ViewKeys = keys
		}
	}

	q.ViewStartKey, q.ViewEndKey, q.ViewDecodedStartKey, q.ViewDecodedEndKey, q.ViewExclusiveEnd = viewKeyRange(options)

	// In descending mode the endkey is the lower bound; strip the 0xFF
	// padding that viewKeyRange added for inclusive forward endkey matching.
	// The descending Continue() check uses >= (inclusive) without padding.
	if q.ViewDescending && q.ViewEndKey != nil && !q.ViewExclusiveEnd {
		if len(q.ViewEndKey) >= 10 {
			q.ViewEndKey = q.ViewEndKey[:len(q.ViewEndKey)-10]
		}
	}

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

		rows := make([]Rows, 0, len(docs))
		for key, value := range docs {
			var row Rows
			if doc, ok := value.(*model.Document); ok {
				row.ID = doc.ID
				row.Key = doc.Key
				row.Value = doc.Value
			} else {
				row.Key = key
				row.Value = value
			}
			rows = append(rows, row)
		}

		// Filter to requested keys if provided.
		if len(q.ViewKeys) > 0 {
			rows = filterRowsByKeys(rows, q.ViewKeys)
		}

		// Sort: ascending or descending.
		if q.ViewDescending {
			sort.Slice(rows, func(i, j int) bool {
				return model.ViewKeyCmp(rows[i].Key, rows[j].Key) > 0
			})
		} else {
			sort.Slice(rows, func(i, j int) bool {
				return model.ViewKeyCmp(rows[i].Key, rows[j].Key) < 0
			})
		}

		response := buildViewResponse(rows, total, q, db, r)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response) // nolint: errcheck
	} else {
		var docList []*model.Document

		if len(q.ViewKeys) > 0 {
			// Multi-key lookup: iterate for each key independently.
			err = db.Transaction(r.Context(), func(tx port.DatabaseTx) error {
				for _, k := range q.ViewKeys {
					sk, ek := cborKeyRange(k)
					if sk == nil {
						continue
					}
					iter, iterErr := db.IndexIterator(r.Context(), tx, idx)
					if iterErr != nil {
						return iterErr
					}
					iter.SetStartKey(sk)
					iter.SetEndKey(ek)
					iter.SetExclusiveEnd(false)
					for doc := iter.First(); iter.Continue(); doc = iter.Next() {
						docList = append(docList, doc)
					}
				}
				total = len(docList) // total = total results for key lookup
				return nil
			})
		} else {
			err = db.Transaction(r.Context(), func(tx port.DatabaseTx) error {
				iter, iterErr := db.IndexIterator(r.Context(), tx, idx)
				if iterErr != nil {
					return iterErr
				}

				iter.SetSkip(int(q.Skip))
				iter.SetLimit(int(q.Limit))
				iter.SetDescending(q.ViewDescending)
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
					if q.ViewDescending {
						if q.ViewDecodedStartKey != nil && model.ViewKeyCmp(doc.Key, q.ViewDecodedStartKey) > 0 {
							continue
						}
						if q.ViewDecodedEndKey != nil {
							cmp := model.ViewKeyCmp(doc.Key, q.ViewDecodedEndKey)
							if q.ViewExclusiveEnd && cmp <= 0 {
								continue
							} else if !q.ViewExclusiveEnd && cmp < 0 {
								continue
							}
						}
					} else {
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
					}
					docList = append(docList, doc)
				}
				total = iter.Remaining() + len(docList)

				return iterErr
			})
		}

		// Enrich documents with full data if include_docs=true
		if err == nil && q.IncludeDocs && len(docList) > 0 {
			err = db.EnrichDocuments(r.Context(), docList)
		}
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Sort result rows.
		if q.ViewDescending && len(q.ViewKeys) == 0 {
			sort.Slice(docList, func(i, j int) bool {
				return model.ViewKeyCmp(docList[i].Key, docList[j].Key) > 0
			})
		} else {
			sort.Slice(docList, func(i, j int) bool {
				return model.ViewKeyCmp(docList[i].Key, docList[j].Key) < 0
			})
		}

		rows := make([]Rows, len(docList))
		for i, doc := range docList {
			rows[i].ID = doc.ID
			rows[i].Key = doc.Key
			rows[i].Value = doc.Value
			if q.IncludeDocs && doc.Data != nil {
				rows[i].Doc = doc.Data
				rows[i].Doc["_id"] = doc.ID
				rows[i].Doc["_rev"] = doc.Rev
				if doc.Deleted {
					rows[i].Doc["_deleted"] = doc.Deleted
				}
			}
		}

		response := buildViewResponse(rows, total, q, db, r)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response) // nolint: errcheck
	}
}

// buildViewResponse constructs a ViewResponse honoring sorted and update_seq flags.
func buildViewResponse(rows []Rows, total int, q port.AllDocsQuery, db port.Database, r *http.Request) ViewResponse {
	response := ViewResponse{
		Rows: rows,
	}
	if !q.ViewOmitSortedInfo {
		response.TotalRows = &total
		offset := int(q.Skip)
		response.Offset = &offset
	}
	if q.ViewUpdateSeq {
		// The update_seq is the current sequence of the _changes bucket, which
		// is incremented each time a document is modified (via PutWithSequence).
		// db.Sequence() reads the docs bucket sequence (always 0); instead we
		// read directly from the _changes bucket inside a read transaction.
		var updateSeq int64
		_ = db.Transaction(r.Context(), func(tx port.DatabaseTx) error {
			updateSeq = int64(tx.Sequence([]byte("_changes")))
			return nil
		})
		response.UpdateSeq = updateSeq
	}
	return response
}

// filterRowsByKeys returns only those rows whose key equals one of the
// requested keys (using ViewKeyCmp for CouchDB collation-correct matching).
func filterRowsByKeys(rows []Rows, keys []interface{}) []Rows {
	out := make([]Rows, 0, len(rows))
	for _, row := range rows {
		for _, k := range keys {
			if model.ViewKeyCmp(row.Key, k) == 0 {
				out = append(out, row)
				break
			}
		}
	}
	return out
}
