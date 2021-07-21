package index

import (
	"github.com/goydb/goydb/pkg/model"
)

const DeletedIndexName = "_deleted"

func NewDeletedIndex() *UniqueIndex {
	return NewUniqueIndex(
		DeletedIndexName,
		// key is the local sequence of the document
		func(doc *model.Document) []byte {
			if !doc.Deleted {
				return nil
			}
			return []byte(doc.ID)
		},
		// value is a bson marshaled document
		// of the most important fields for following
		// the changes feed
		func(doc *model.Document) []byte {
			return nil
		},
	)
}
