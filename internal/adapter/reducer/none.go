package reducer

import (
	"github.com/goydb/goydb/pkg/model"
)

type None struct {
	result map[interface{}]interface{}
}

func NewNone() *Count {
	return &Count{
		result: make(map[interface{}]interface{}),
	}
}

func (r *None) Reduce(doc *model.Document) {
	r.result[doc.ID] = doc
}

func (r *None) Result() map[interface{}]interface{} {
	return r.result
}
