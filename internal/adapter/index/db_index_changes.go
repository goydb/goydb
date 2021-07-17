package index

import (
	"github.com/goydb/goydb/pkg/model"
	"gopkg.in/mgo.v2/bson"
)

const ChangesIndexName = "_changes"

func NewChangesIndex(name string) *UniqueIndexUint64 {
	return NewUniqueIndexUint64(
		name,
		// key is the local sequence of the document
		func(doc *model.Document) uint64 {
			return doc.LocalSeq
		},
		// value is a bson marshaled document
		// of the most important fields for following
		// the changes feed
		func(doc *model.Document) []byte {
			out, err := bson.Marshal(&model.Document{
				ID:       doc.ID,
				Rev:      doc.Rev,
				LocalSeq: doc.LocalSeq,
				Deleted:  doc.Deleted,
			})
			if err != nil {
				return nil
			}
			return out
		},
	)
}
