package gojaview

import (
	"fmt"
	"log"

	"github.com/dop251/goja"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const reduceOver = 100

type Reducer struct {
	vm          *goja.Runtime
	reducedDocs []*model.Document
	docs        []*model.Document
	reduceOver  int
}

func NewReducer(source string) (port.Reducer, error) {
	vm := goja.New()
	fn := `
	var _result = [];
	var _keys = [];
	var _values = [];
	var rereduce = false;
	function sum(values) {
		var _sum = 0;
		values.forEach(function (value) {
			_sum += value
		});
		return _sum;
	}`
	vm.Set("println", fmt.Println)
	_, err := vm.RunString(fn)
	if err != nil {
		return nil, fmt.Errorf("script error %v: %w", fn, err)
	}
	_, err = vm.RunScript("reducer.js", "var reduceFn = "+source+";")
	if err != nil {
		return nil, fmt.Errorf("script error %v: %w", fn, err)
	}

	return &Reducer{
		vm:         vm,
		reduceOver: reduceOver,
	}, nil
}

func (r *Reducer) Reduce(doc *model.Document, group bool) {
	r.docs = append(r.docs, doc)

	if len(r.docs) > 0 && len(r.docs)%r.reduceOver == 0 {
		r.reduce(false)
	}
}

func (r *Reducer) reduce(rereduce bool) {
	r.vm.Set("rereduce", rereduce)
	var docs []*model.Document

	if rereduce {
		docs = r.reducedDocs
		r.reducedDocs = nil
	} else {
		docs = r.docs
		r.docs = nil
	}

	keys := make([]interface{}, len(docs))
	values := make([]interface{}, len(docs))
	for i, doc := range docs {
		keys[i] = doc.Key
		values[i] = doc.Value
	}
	r.vm.Set("_keys", keys)
	r.vm.Set("_values", values)
	_, err := r.vm.RunString(`_result = reduceFn(_keys, _values, rereduce);`)
	if err != nil {
		log.Printf("JS ERR: #v", err)
	}

	resultData, ok := r.vm.Get("_result").Export().(interface{})
	if !ok {
		log.Printf("JS ERR: unable to export")
	}

	// fmt.Println(resultData)
	r.reducedDocs = append(r.reducedDocs, &model.Document{
		Key:   nil,
		Value: resultData,
	})
}

func (r *Reducer) Result() []*model.Document {
	if len(r.docs) != 0 {
		r.reduce(false)
	}
	r.reduce(true) // final rereduce
	return r.reducedDocs
}
