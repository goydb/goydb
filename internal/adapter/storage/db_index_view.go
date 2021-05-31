package storage

import (
	"fmt"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

var _ port.DocumentIndex = (*ViewIndex)(nil)

type ViewIndex struct {
	*RegularIndex
	MapFn, ReduceFn string
}

func NewViewIndex(ddfn *model.DesignDocFn, mapFn, reduceFn string) *ViewIndex {
	vi := &ViewIndex{
		MapFn:    mapFn,
		ReduceFn: mapFn,
	}

	vi.RegularIndex = NewRegularIndex(ddfn, vi.indexSingleDocument)

	return vi
}

func (i *ViewIndex) String() string {
	return fmt.Sprintf("<ViewIndex name=%q>", i.RegularIndex.ddfn)
}

func (i *ViewIndex) indexSingleDocument(doc *model.Document) ([][]byte, [][]byte) {
	// ############### DUMMY IMPLEMENTATION OF SOME ALG ################

	var keys, values [][]byte

	// index all values
	for k, v := range doc.Data {
		keys = append(keys, []byte(k))
		out, err := bson.Marshal(model.Document{
			ID:    doc.ID,
			Value: v,
		})
		if err == nil {
			values = append(values, out)
		}
	}

	return keys, values
}

// updateSource updates the view source and starts
// rebuilding the whole index
func (i *ViewIndex) updateSource(mapFn, reduceFn string) error {
	panic("not implemented") // TODO: Implement
}
