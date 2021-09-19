package controller

import (
	"context"

	"github.com/goydb/goydb/internal/adapter/reducer"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DesignDoc struct {
	DB *storage.Database
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

		err = v.DB.Transaction(ctx, func(tx *storage.Transaction) error {
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

func (v DesignDoc) ReduceDocs(ctx context.Context, tx *storage.Transaction, idx port.DocumentIndex, opts port.AllDocsQuery, view *model.View) (map[interface{}]interface{}, int, error) {
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
	total = i.Total()
	if total == 0 {
		return nil, 0, nil
	}
	for doc := i.First(); i.Continue(); doc = i.Next() {
		// if the view group is 0 all values should be treated without the key
		if opts.ViewGroup == "0" {
			doc.Key = nil
		}

		// TODO: implement other group levels using 1-10 if key is
		// an array

		r.Reduce(doc)
	}

	docs := r.Result()
	return docs, total, nil
}
