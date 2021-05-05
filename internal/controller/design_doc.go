package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/adapter/view/tengoview"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var ErrNoViewFunctions = errors.New("no view functions in the document")

type ReducerFunc func(docs []*model.Document, doc *model.Document, group bool) []*model.Document

type DesignDoc struct {
	DB        port.Database
	SourceDoc *model.Document
	FnName    string
	Doc       *model.Document
}

func (v DesignDoc) Reset(ctx context.Context) error {
	if v.Doc != nil {
		return v.DB.ResetViewIndexForDoc(ctx, v.Doc.ID)
	}
	if v.SourceDoc != nil {
		vfns := v.SourceDoc.ViewFunctions()
		if len(vfns) == 0 {
			return nil
		}
		for _, vfn := range vfns {
			ddfn := model.DesignDocFn{Type: model.ViewFn, DesignDocID: v.SourceDoc.ID, FnName: vfn.Name}
			err := v.DB.ResetView(ctx, &ddfn)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (v DesignDoc) ViewServer(fn string) (port.ViewServer, error) {
	lang := v.SourceDoc.Language()
	switch lang {
	case "javascript", "":
		return gojaview.NewViewServer(fn)
	case "tengo":
		return tengoview.NewViewServer(fn)
	default:
		return nil, fmt.Errorf("Language %q unkown", lang)
	}
}

func (v DesignDoc) GetViewServer() (map[string]port.ViewServer, error) {
	allViewServer := make(map[string]port.ViewServer)
	vfns := v.SourceDoc.ViewFunctions()

	for _, vfn := range vfns {
		// filter for specific view function
		if v.FnName != "" && vfn.Name != v.FnName {
			continue
		}

		// create view server
		server, err := v.ViewServer(vfn.MapFn)
		if err != nil {
			return nil, fmt.Errorf("compile error for %q: %w", vfn.Name, err)
		}
		allViewServer[vfn.Name] = server
	}

	return allViewServer, nil
}

func (v DesignDoc) GetSearchServer() (map[string]port.ViewServer, error) {
	allViewServer := make(map[string]port.ViewServer)
	sfns := v.SourceDoc.SearchFunctions()

	for _, sfn := range sfns {
		// filter for specific view function
		if v.FnName != "" && sfn.Name != v.FnName {
			continue
		}

		// create view server
		server, err := v.ViewServer(sfn.SearchFn)
		if err != nil {
			return nil, fmt.Errorf("compile error for %q: %w", sfn.Name, err)
		}
		allViewServer[sfn.Name] = server
	}

	return allViewServer, nil
}

func (v DesignDoc) Rebuild(ctx context.Context, task *model.Task) error {
	ddfn := model.DesignDocFn{DesignDocID: v.SourceDoc.ID}
	vfns, err := v.GetViewServer()
	if err != nil {
		return fmt.Errorf("failed to compile view function: %w", err)
	}

	sfns, err := v.GetSearchServer()
	if err != nil {
		return fmt.Errorf("failed to compile search function: %w", err)
	}

	fns := len(vfns) + len(sfns)
	if fns == 0 {
		return nil
	}

	j := 0
	batchSize := 1000
	for {
		var docs []*model.Document
		if v.Doc == nil {
			err := v.DB.Iterator(ctx, nil, func(i port.Iterator) error {
				total := i.Total()
				if total == 0 {
					return nil
				}
				if task != nil {
					if v.FnName == "" { // no single view update
						task.ProcessingTotal = total * fns
					} else {
						task.ProcessingTotal = total
					}
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
		} else {
			docs = []*model.Document{v.Doc}
		}

		if task != nil {
			task.Processed += len(docs)
			err := v.DB.UpdateTask(ctx, task)
			if err != nil {
				return err
			}
		}

		// update views
		for vfnName, vfnServer := range vfns {
			ddfn.Type = model.ViewFn
			ddfn.FnName = vfnName
			viewDocs, err := vfnServer.ExecuteView(ctx, docs)
			if err != nil {
				return err
			}

			err = v.DB.UpdateView(ctx, &ddfn, viewDocs)
			if err != nil {
				return err
			}
		}

		// update search functions
		for sfnName, sfnServer := range sfns {
			ddfn.Type = model.SearchFn
			ddfn.FnName = sfnName
			searchDocs, err := sfnServer.ExecuteSearch(ctx, docs)
			if err != nil {
				return err
			}
			_ = sfnServer

			err = v.DB.UpdateSearch(ctx, &ddfn, searchDocs)
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

func (v DesignDoc) ViewFunctions() (*model.ViewFunction, error) {
	if v.FnName == "" {
		panic("can only be called with ViewName defined")
	}

	vfns := v.SourceDoc.ViewFunctions()
	if vfns == nil {
		return nil, ErrNoViewFunctions
	}
	if len(vfns) == 0 {
		return nil, nil
	}

	for _, vfn := range vfns {
		// filter for specific view function
		if v.FnName != "" && vfn.Name != v.FnName {
			continue
		}

		return vfn, nil
	}

	return nil, fmt.Errorf("view function %q not found", v.FnName)
}

func (v DesignDoc) ReduceDocs(ctx context.Context, opts port.AllDocsQuery) ([]*model.Document, int, error) {
	vfn, err := v.ViewFunctions()
	if err != nil {
		return nil, 0, err
	}

	var reducer ReducerFunc
	switch vfn.ReduceFn {
	case "_sum":
		reducer = _sum
	case "_count":
		reducer = _count
	case "_stats":
		// source: https://docs.couchdb.org/en/stable/ddocs/ddocs.html?highlight=_stats#built-in-reduce-functions
		/*server, err = v.ViewServer(`
		function(keys, values, rereduce) {
			if (rereduce) {
				return {
					'sum': values.reduce(function(a, b) { return a + b.sum }, 0),
					'min': values.reduce(function(a, b) { return Math.min(a, b.min) }, Infinity),
					'max': values.reduce(function(a, b) { return Math.max(a, b.max) }, -Infinity),
					'count': values.reduce(function(a, b) { return a + b.count }, 0),
					'sumsqr': values.reduce(function(a, b) { return a + b.sumsqr }, 0)
				}
			} else {
				return {
					'sum': sum(values),
					'min': Math.min.apply(null, values),
					'max': Math.max.apply(null, values),
					'count': values.length,
					'sumsqr': (function() {
					var sumsqr = 0;

					values.forEach(function (value) {
						sumsqr += value * value;
					});

					return sumsqr;
					})(),
				}
			}
		}`)*/
		panic("not implemented")
	case "_approx_count_distinct":
		// FIXME: this is not giving the same speed
		// but the correctness
		reducer = _count
	case "": // NONE
		reducer = func(docs []*model.Document, doc *model.Document, group bool) []*model.Document {
			return append(docs, doc)
		}
	default: // CUSTOM
		// TODO: use view server
		// create view server
		/*server, err := v.ViewServer(vfn.ReduceFn)
		if err != nil {
			return err
		}*/
		panic("not implemented")
	}

	var total int
	var docs []*model.Document
	ddfn := model.DesignDocFn{Type: model.ViewFn, DesignDocID: v.SourceDoc.ID, FnName: v.FnName}
	err = v.DB.Iterator(ctx, &ddfn, func(i port.Iterator) error {
		total = i.Total()
		if total == 0 {
			return nil
		}

		for doc := i.First(); i.Continue(); doc = i.Next() {
			docs = reducer(docs, doc, opts.ViewGroup)
		}

		return nil

	})
	if err != nil {
		return nil, 0, err
	}
	if !opts.ViewGroup && len(docs) == 1 {
		docs[0].Key = nil
	}

	return docs, total, nil
}

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
