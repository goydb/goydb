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

func (v DesignDoc) ReduceDocs(ctx context.Context, tx *storage.Transaction, idx port.DocumentIndex, opts port.AllDocsQuery, view *model.View) ([]*model.Document, int, error) {
	var r port.Reducer
	switch view.ReduceFn {
	case "_sum":
		r = new(reducer.Sum)
	case "_count":
		r = new(reducer.Count)
	case "_stats":
		r = reducer.NewStats()
	case "_approx_count_distinct":
		// FIXME: this is not giving the same speed but the correctness
		r = new(reducer.Count)
	case "": // NONE
		r = new(reducer.None)
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
		r.Reduce(doc, opts.ViewGroup)
	}

	docs := r.Result()
	if !opts.ViewGroup && len(docs) == 1 {
		docs[0].Key = nil
	}
	return docs, total, nil
}
