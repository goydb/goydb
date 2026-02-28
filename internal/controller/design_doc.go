package controller

import (
	"context"

	"github.com/goydb/goydb/internal/adapter/reducer"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DesignDoc struct {
	DB port.Database
}

func (v DesignDoc) Rebuild(ctx context.Context, task *model.Task, idx port.DocumentIndex) error {
	j := 0
	batchSize := 1000
	for {
		var docs []*model.Document
		err := v.DB.Iterator(ctx, nil, func(i port.Iterator) error {
			total := i.Total()
			if total == 0 {
				return nil
			}
			if task != nil {
				task.ProcessingTotal = total
			}

			i.SetSkip(j * batchSize)
			i.SetLimit(batchSize)
			i.SetSkipDesignDoc(true)
			i.SetSkipLocalDoc(true)

			for doc := i.First(); i.Continue(); doc = i.Next() {
				docs = append(docs, doc)
			}

			return nil

		})
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			break
		}

		err = v.DB.Transaction(ctx, func(tx port.DatabaseTx) error {
			return idx.UpdateStored(ctx, tx, docs)
		})
		if err != nil {
			return err
		}

		if task != nil {
			task.Processed += len(docs)
			err := v.DB.UpdateTask(ctx, task)
			if err != nil {
				return err
			}
		}

		j++
		if len(docs) < batchSize {
			break
		}
	}

	return nil
}

func (v DesignDoc) ReduceDocs(ctx context.Context, tx port.EngineReadTransaction, idx port.DocumentIndex, opts port.AllDocsQuery, view *model.View) (map[interface{}]interface{}, int, error) {
	var r port.Reducer
	switch view.ReduceFn {
	case "_sum":
		r = reducer.NewSum()
	case "_count":
		r = reducer.NewCount()
	case "_stats":
		r = reducer.NewStats()
	case "_approx_count_distinct":
		// FIXME: this is not giving the same speed but the correctness
		r = new(reducer.Count)
	case "": // NONE
		r = reducer.NewNone()
	default: // CUSTOM
		var err error
		r, err = v.DB.ReducerEngine(view.Language)(view.ReduceFn)
		if err != nil {
			return nil, 0, err
		}
	}

	var total int
	io, err := idx.IteratorOptions(ctx)
	if err != nil {
		return nil, 0, err
	}
	i := storage.NewIterator(tx, storage.WithOptions(io))
	// Do NOT set iterator bounds (SetStartKey/SetEndKey) — CBOR byte ordering
	// diverges from CouchDB collation for strings of different lengths.
	// Instead, iterate the full index and rely on the semantic post-filter below.
	total = i.Total()
	if total == 0 {
		return nil, 0, nil
	}
	for doc := i.First(); i.Continue(); doc = i.Next() {
		// Semantic post-filter using CouchDB collation (CBOR byte order diverges
		// from CouchDB collation order for strings of different lengths).
		if opts.ViewDescending {
			// Descending: startkey is upper bound, endkey is lower bound.
			if opts.ViewDecodedStartKey != nil && model.ViewKeyCmp(doc.Key, opts.ViewDecodedStartKey) > 0 {
				continue
			}
			if opts.ViewDecodedEndKey != nil {
				cmp := model.ViewKeyCmp(doc.Key, opts.ViewDecodedEndKey)
				if opts.ViewExclusiveEnd && cmp <= 0 {
					continue
				} else if !opts.ViewExclusiveEnd && cmp < 0 {
					continue
				}
			}
		} else {
			// Ascending: startkey is lower bound, endkey is upper bound.
			if opts.ViewDecodedStartKey != nil && model.ViewKeyCmp(doc.Key, opts.ViewDecodedStartKey) < 0 {
				continue
			}
			if opts.ViewDecodedEndKey != nil {
				cmp := model.ViewKeyCmp(doc.Key, opts.ViewDecodedEndKey)
				if opts.ViewExclusiveEnd && cmp >= 0 {
					continue
				} else if !opts.ViewExclusiveEnd && cmp > 0 {
					continue
				}
			}
		}

		if opts.StartKeyDocID != "" && doc.ID < opts.StartKeyDocID {
			continue
		}
		if opts.EndKeyDocID != "" && doc.ID > opts.EndKeyDocID {
			continue
		}

		// Apply group / group_level key reduction before passing to the reducer.
		// Map-only views (no ReduceFn) skip this: they preserve keys like reduce=false.
		if view.ReduceFn != "" {
			if opts.ViewGroupLevel > 0 {
				if arr, ok := doc.Key.([]interface{}); ok && opts.ViewGroupLevel < len(arr) {
					doc.Key = arr[:opts.ViewGroupLevel]
				}
				// non-array key or key shorter/equal to group_level: keep as-is
			} else if opts.ViewGroup != "true" {
				// default: collapse all keys to nil → single aggregated row
				doc.Key = nil
			}
			// group=true: keep full key unchanged
		}

		r.Reduce(doc)
	}

	docs := r.Result()
	return docs, total, nil
}
