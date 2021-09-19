package reducer

import (
	"github.com/goydb/goydb/pkg/model"
)

type None struct {
	docs []*model.Document
}

func (r *None) Reduce(doc *model.Document, group bool) {
	r.docs = append(r.docs, doc)
}

func (r *None) Result() []*model.Document {
	return r.docs
}
