package storage

import (
	"encoding/binary"
	"strconv"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type UniqueIndexUint64KeyFunc func(doc *model.Document) uint64

type UniqueIndexUint64 struct {
	UniqueIndex
}

// NewUniqueIndexUint64 creates a sorted uint64 index
// that can be scanned in order using the iterator
func NewUniqueIndexUint64(name string, kf UniqueIndexUint64KeyFunc, value IndexFunc) port.DocumentIndex {
	bkf := func(doc *model.Document) []byte {
		return uint64ToKey(kf(doc))
	}

	return &UniqueIndexUint64{
		UniqueIndex: UniqueIndex{
			bucketName:  []byte(name),
			key:         bkf,
			value:       value,
			iterKeyFunc: byteToUint64Key,
		},
	}
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
