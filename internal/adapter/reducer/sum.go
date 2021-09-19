package reducer

import (
	"reflect"

	"github.com/goydb/goydb/pkg/model"
)

type Sum struct {
	docs []*model.Document
}

func (r *Sum) Reduce(doc *model.Document, group bool) {
	docs := r.docs

	v, ok := doc.Value.(int64)
	if ok {
		if len(docs) == 0 {
			docs = []*model.Document{
				{
					Key:   doc.Key,
					Value: v,
				},
			}
		} else {
			i := len(docs) - 1
			if group && !reflect.DeepEqual(docs[i].Key, doc.Key) {
				docs = append(docs, &model.Document{
					Key:   doc.Key,
					Value: v,
				})
				i++
			}
			docs[i].Value = docs[i].Value.(int64) + v
		}
	}

	r.docs = docs
}

func (r *Sum) Result() []*model.Document {
	return r.docs
}
