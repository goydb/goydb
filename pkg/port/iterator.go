package port

import "github.com/goydb/goydb/pkg/model"

type Iterator interface {
	Total() int
	First() *model.Document
	Next() *model.Document
	IncLimit()
	Continue() bool
	Remaining() int

	SetLimit(int)
	SetStartKey([]byte)
	SetEndKey([]byte)
	SetSkip(int)
	SetSkipLocalDoc(bool)
	SetSkipDesignDoc(bool)
}
