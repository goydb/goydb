package gojaview

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.ViewServer = (*ViewServer)(nil)

type ViewServer struct {
	vm *goja.Runtime
}

func NewViewServer(fn string) (port.ViewServer, error) {
	vm := goja.New()
	fn = `
	var _result = [];
	var _doc = {};
	var docs = [];
	function emit(key, value) {
		_result.push([key, value, _doc._id]);
	}
	var docFn = ` + fn + `;`
	_, err := vm.RunString(fn)
	if err != nil {
		return nil, fmt.Errorf("script error %v: %w", fn, err)
	}

	return &ViewServer{
		vm: vm,
	}, nil
}

func (s *ViewServer) ExecuteView(ctx context.Context, docs []*model.Document) ([]*model.Document, error) {
	simpleDocs := make([]interface{}, len(docs))
	for i, doc := range docs {
		doc.Data["_id"] = doc.ID
		doc.Data["_rev"] = doc.Rev
		simpleDocs[i] = doc.Data
	}

	s.vm.Set("docs", simpleDocs)

	_, err := s.vm.RunString(`_result = [];
	docs.forEach(function (doc) {
		_doc = doc;
		docFn(doc);
	});`)
	if err != nil {
		return nil, err
	}

	resultData, ok := s.vm.Get("_result").Export().([]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to export")
	}
	result := make([]*model.Document, len(resultData))

	for i, rd := range resultData {
		row := rd.([]interface{})
		// fmt.Println(i, row)
		result[i] = &model.Document{
			Key:   row[0],
			Value: row[1],
			ID:    row[2].(string),
		}
	}

	return result, nil
}

func (s *ViewServer) ExecuteSearch(ctx context.Context, docs []*model.Document) ([]*model.Document, error) {
	// TODO implement search execution in javascript
	panic("not implemented") // TODO: Implement
}
