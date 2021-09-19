package gojaview

import (
	"fmt"
	"log"
	"reflect"

	"github.com/dop251/goja"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const reduceOver = 1000

type Reducer struct {
	vm          *goja.Runtime
	reducedDocs []*model.Document
	keys        []interface{}
	values      []interface{}
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
			_sum += value;
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

func (r *Reducer) Reduce(doc *model.Document) {
	r.reduceDoc(doc, false)
}

func (r *Reducer) reduceDoc(doc *model.Document, rereduce bool) {
	tooManyElements := len(r.keys) > 0 && len(r.keys)%r.reduceOver == 0
	keyChange := len(r.keys) > 0 && !reflect.DeepEqual(r.keys[len(r.keys)-1], doc.Key)

	if tooManyElements || keyChange {
		r.reduce(rereduce)
	}

	r.keys = append(r.keys, doc.Key)
	r.values = append(r.values, doc.Value)
}

func (r *Reducer) reduce(rereduce bool) {
	keys := r.keys
	values := r.values
	r.keys = nil
	r.values = nil

	r.vm.Set("rereduce", rereduce)
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

	r.reducedDocs = append(r.reducedDocs, &model.Document{
		Key:   keys[0],
		Value: resultData,
	})
}

func (r *Reducer) Result() map[interface{}]interface{} {
	// check if a reduce need to happen because there
	// are still keys and values not reduced
	if len(r.keys) != 0 {
		r.reduce(false)
	}

	// add all reduced docs to as preperation
	// for the rereduce step
	for _, doc := range r.reducedDocs {
		r.reduceDoc(doc, true)
	}

	// final rereduce
	r.reduce(true)

	// reformat the output
	result := make(map[interface{}]interface{})
	for _, doc := range r.reducedDocs {
		result[doc.Key] = doc.Value
	}

	return result
}
