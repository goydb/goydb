package reducer

import (
	"reflect"

	"github.com/goydb/goydb/pkg/model"
)

type Count struct {
	docs []*model.Document
}

func (r *Count) Reduce(doc *model.Document, group bool) {
	docs := r.docs

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

	r.docs = docs
}

func (r *Count) Result() []*model.Document {
	return r.docs
}
