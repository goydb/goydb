package index

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/goydb/goydb/pkg/model"
)

type UniqueIndexUint64KeyFunc func(doc *model.Document) uint64

type UniqueIndexUint64 struct {
	UniqueIndex
}

// NewUniqueIndexUint64 creates a sorted uint64 index
// that can be scanned in order using the iterator
func NewUniqueIndexUint64(name string, kf UniqueIndexUint64KeyFunc, value IndexFunc) *UniqueIndexUint64 {
	bkf := func(doc *model.Document) []byte {
		return uint64ToKey(kf(doc))
	}

	return &UniqueIndexUint64{
		UniqueIndex: UniqueIndex{
			bucketName:  []byte(name),
			key:         bkf,
			value:       value,
			iterKeyFunc: byteToUint64Key,
			// key is a bigint binary, convert back to base10 string
			cleanKey: func(b []byte) string {
				ui := binary.BigEndian.Uint64(b)
				return strconv.FormatUint(ui, 10)
			},
		},
	}
}

func (i *UniqueIndexUint64) String() string {
	return fmt.Sprintf("<UniqueIndexUint64 name=%q>", string(i.UniqueIndex.bucketName))
}

// uint64ToKey big endian bytes of passed v
func uint64ToKey(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// byteToUint64Key parses the passed v as integer string
// and then returns the big endian encoding (byte) for it
func byteToUint64Key(v []byte) []byte {
	ui, err := strconv.ParseUint(string(v), 10, 64)
	if err != nil {
		return nil
	}
	return uint64ToKey(ui)
}
