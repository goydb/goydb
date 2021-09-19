package reducer

import (
	"github.com/goydb/goydb/pkg/model"
)

type Count struct {
	result map[interface{}]interface{}
}

func NewCount() *Count {
	return &Count{
		result: make(map[interface{}]interface{}),
	}
}

func (r *Count) Reduce(doc *model.Document) {
	value, ok := r.result[doc.Key]

	if ok {
		r.result[doc.Key] = value.(int64) + 1
	} else {
		r.result[doc.Key] = int64(1)
	}
}

func (r *Count) Result() map[interface{}]interface{} {
	return r.result
}
