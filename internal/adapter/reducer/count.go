package reducer

import (
	"reflect"

	"github.com/goydb/goydb/pkg/model"
)

type Count struct {
	keys   []interface{}
	counts []int64
}

func NewCount() *Count {
	return &Count{}
}

func (r *Count) indexOf(key interface{}) int {
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

func (r *Count) Reduce(doc *model.Document) {
	idx := r.indexOf(doc.Key)
	if idx >= 0 {
		r.counts[idx]++
	} else {
		r.keys = append(r.keys, doc.Key)
		r.counts = append(r.counts, 1)
	}
}

func (r *Count) Result() map[interface{}]interface{} {
	out := make(map[interface{}]interface{}, len(r.keys))
	for i, k := range r.keys {
		out[i] = &model.Document{Key: k, Value: r.counts[i]}
	}
	return out
}
