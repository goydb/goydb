package reducer

import (
	"reflect"

	"github.com/goydb/goydb/pkg/model"
)

type Sum struct {
	keys   []interface{}
	values []interface{}
}

func NewSum() *Sum {
	return &Sum{}
}

func (r *Sum) indexOf(key interface{}) int {
	n := len(r.keys)
	if n > 0 && reflect.DeepEqual(r.keys[n-1], key) {
		return n - 1
	}
	for i, k := range r.keys {
		if reflect.DeepEqual(k, key) {
			return i
		}
	}
	return -1
}

// Reduce will sum up using int64 if integer values are used,
// and switch to float64 as soon as decimal values are used.
func (r *Sum) Reduce(doc *model.Document) {
	idx := r.indexOf(doc.Key)
	if idx >= 0 {
		r.values[idx] = sumAdd(r.values[idx], doc.Value)
	} else {
		r.keys = append(r.keys, doc.Key)
		r.values = append(r.values, doc.Value)
	}
}

// sumAdd handles int64+int64, int64+float64, float64+int64, float64+float64.
func sumAdd(cur, add interface{}) interface{} {
	if ci, ok := cur.(int64); ok {
		if ai, ok := add.(int64); ok {
			return ci + ai
		}
		if af, ok := add.(float64); ok {
			return float64(ci) + af
		}
	}
	if cf, ok := cur.(float64); ok {
		if ai, ok := add.(int64); ok {
			return cf + float64(ai)
		}
		if af, ok := add.(float64); ok {
			return cf + af
		}
	}
	return cur
}

func (r *Sum) Result() map[interface{}]interface{} {
	out := make(map[interface{}]interface{}, len(r.keys))
	for i, k := range r.keys {
		out[i] = &model.Document{Key: k, Value: r.values[i]}
	}
	return out
}
