package gojaview

import (
	"context"
	"fmt"
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
	docIDs      []string // parallel to keys; only populated during first-pass reduce
	values      []interface{}
	reduceOver  int
	logger      port.Logger
}

// NewReducerBuilder returns a ReducerServerBuilder that captures the logger
func NewReducerBuilder(logger port.Logger) port.ReducerServerBuilder {
	return func(source string) (port.Reducer, error) {
		return NewReducer(source, logger)
	}
}

func NewReducer(source string, logger port.Logger) (port.Reducer, error) {
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
	_ = vm.Set("println", fmt.Println)
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
		logger:     logger,
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
	if !rereduce {
		r.docIDs = append(r.docIDs, doc.ID)
	}
	r.values = append(r.values, doc.Value)
}

func (r *Reducer) reduce(rereduce bool) {
	keys := r.keys
	docIDs := r.docIDs
	values := r.values
	r.keys = nil
	r.docIDs = nil
	r.values = nil

	// CouchDB passes [[key, docId], ...] pairs for first-pass reduce and null
	// for rereduce.  Reducers that use keys[i][1] to get the doc ID (a common
	// CouchDB pattern) will receive undefined / TypeError if we pass raw keys.
	var jsKeys interface{}
	if !rereduce {
		pairs := make([]interface{}, len(keys))
		for i, k := range keys {
			pairs[i] = []interface{}{k, docIDs[i]}
		}
		jsKeys = pairs
	} // rereduce: jsKeys stays nil → JS null

	_ = r.vm.Set("rereduce", rereduce)
	_ = r.vm.Set("_keys", jsKeys)
	_ = r.vm.Set("_values", values)
	_, err := r.vm.RunString(`_result = reduceFn(_keys, _values, rereduce);`)
	if err != nil {
		r.logger.Errorf(context.Background(), "javascript error", "error", err)
	}

	resultData := r.vm.Get("_result").Export()
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

	// Only rereduce when a key produced more than one intermediate batch result.
	// CouchDB only rereduces when merging B-tree nodes; for small datasets
	// (all docs in a single batch) rereduce is never called.  Unconditionally
	// rereducing causes user reducers that don't handle rereduce correctly to
	// return empty/null values even though a plain reduce would have worked.
	seenKeys := make(map[string]bool, len(r.reducedDocs))
	needsRereduce := false
	for _, doc := range r.reducedDocs {
		canonical := model.ViewKeyString(doc.Key)
		if seenKeys[canonical] {
			needsRereduce = true
			break
		}
		seenKeys[canonical] = true
	}

	if needsRereduce {
		// rereduce using a snapshot of the current reducedDocs to avoid
		// mutating the slice mid-iteration
		snapshot := append([]*model.Document(nil), r.reducedDocs...)
		for _, doc := range snapshot {
			r.reduceDoc(doc, true)
		}

		// final rereduce of whatever remains in keys/values
		if len(r.keys) != 0 {
			r.reduce(true)
		}
	}

	// Build result with deduplication using ViewKeyString to avoid
	// map-key panics for array keys; rereduced values win (last write).
	type kv struct{ k, v interface{} }
	var ordered []kv
	seen := make(map[string]int)
	for _, doc := range r.reducedDocs {
		canonical := model.ViewKeyString(doc.Key)
		if idx, ok := seen[canonical]; ok {
			ordered[idx].v = doc.Value
		} else {
			seen[canonical] = len(ordered)
			ordered = append(ordered, kv{doc.Key, doc.Value})
		}
	}

	result := make(map[interface{}]interface{}, len(ordered))
	for i, e := range ordered {
		result[i] = &model.Document{Key: e.k, Value: e.v}
	}
	return result
}
