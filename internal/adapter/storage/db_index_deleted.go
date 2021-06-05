package storage

import (
	"github.com/goydb/goydb/pkg/model"
)

const DeletedIndexName = "_deleted"

func NewDeletedIndex(name string) *UniqueIndex {
	return NewUniqueIndex(
		name,
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
