package controller

import (
	"context"
	"reflect"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type ReducerFunc func(docs []*model.Document, doc *model.Document, group bool) []*model.Document

type DesignDoc struct {
	DB        *storage.Database
	SourceDoc *model.Document
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

// func (v DesignDoc) ReduceDocs(ctx context.Context, idx port.DocumentIndex, opts port.AllDocsQuery, ReduceFn string) ([]*model.Document, int, error) {
// 	var reducer ReducerFunc
// 	switch ReduceFn {
// 	case "_sum":
// 		reducer = _sum
// 	case "_count":
// 		reducer = _count
// 	case "_stats":
// 		// source: https://docs.couchdb.org/en/stable/ddocs/ddocs.html?highlight=_stats#built-in-reduce-functions
// 		/*server, err = v.ViewServer(`
// 		function(keys, values, rereduce) {
// 			if (rereduce) {
// 				return {
// 					'sum': values.reduce(function(a, b) { return a + b.sum }, 0),
// 					'min': values.reduce(function(a, b) { return Math.min(a, b.min) }, Infinity),
// 					'max': values.reduce(function(a, b) { return Math.max(a, b.max) }, -Infinity),
// 					'count': values.reduce(function(a, b) { return a + b.count }, 0),
// 					'sumsqr': values.reduce(function(a, b) { return a + b.sumsqr }, 0)
// 				}
// 			} else {
// 				return {
// 					'sum': sum(values),
// 					'min': Math.min.apply(null, values),
// 					'max': Math.max.apply(null, values),
// 					'count': values.length,
// 					'sumsqr': (function() {
// 					var sumsqr = 0;

// 					values.forEach(function (value) {
// 						sumsqr += value * value;
// 					});

// 					return sumsqr;
// 					})(),
// 				}
// 			}
// 		}`)*/
// 		panic("not implemented")
// 	case "_approx_count_distinct":
// 		// FIXME: this is not giving the same speed
// 		// but the correctness
// 		reducer = _count
// 	case "": // NONE
// 		reducer = func(docs []*model.Document, doc *model.Document, group bool) []*model.Document {
// 			return append(docs, doc)
// 		}
// 	default: // CUSTOM
// 		// TODO: use view server
// 		// create view server
// 		/*server, err := v.ViewServer(vfn.ReduceFn)
// 		if err != nil {
// 			return err
// 		}*/
// 		panic("not implemented")
// 	}

// 	var total int
// 	var docs []*model.Document

// 	err := idx.Iterator(ctx, &ddfn, func(i port.Iterator) error {
// 		total = i.Total()
// 		if total == 0 {
// 			return nil
// 		}

// 		for doc := i.First(); i.Continue(); doc = i.Next() {
// 			docs = reducer(docs, doc, opts.ViewGroup)
// 		}

// 		return nil

// 	})
// 	if err != nil {
// 		return nil, 0, err
// 	}
// 	if !opts.ViewGroup && len(docs) == 1 {
// 		docs[0].Key = nil
// 	}

// 	return docs, total, nil
// }

func _sum(docs []*model.Document, doc *model.Document, group bool) []*model.Document {
	v, ok := doc.Value.(int64)
	if ok {
		if len(docs) == 0 {
			docs = []*model.Document{
				{
					Key:   doc.Key,
					Value: v,
				},
			}
		} else {
			i := len(docs) - 1
			if group && !reflect.DeepEqual(docs[i].Key, doc.Key) {
				docs = append(docs, &model.Document{
					Key:   doc.Key,
					Value: v,
				})
				i++
			}
			docs[i].Value = docs[i].Value.(int64) + v
		}
	}

	return docs
}

func _count(docs []*model.Document, doc *model.Document, group bool) []*model.Document {
	if len(docs) == 0 {
		docs = []*model.Document{
			{
				Key:   doc.Key,
				Value: int64(1),
			},
		}
	} else {
		i := len(docs) - 1
		if group && !reflect.DeepEqual(docs[i].Key, doc.Key) {
			docs = append(docs, &model.Document{
				Key:   doc.Key,
				Value: int64(0),
			})
			i++
		}
		docs[i].Value = docs[i].Value.(int64) + 1
	}

	return docs
}
