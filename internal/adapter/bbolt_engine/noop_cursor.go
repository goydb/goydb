package bbolt_engine

import "github.com/goydb/goydb/pkg/port"

var _ port.EngineCursor = (*NoopCursor)(nil)

type NoopCursor struct {
}

func (nc *NoopCursor) First() (key []byte, value []byte) {
	return nil, nil
}

func (nc *NoopCursor) Last() (key []byte, value []byte) {
	return nil, nil
}

func (nc *NoopCursor) Next() (key []byte, value []byte) {
	return nil, nil
}

func (nc *NoopCursor) Prev() (key []byte, value []byte) {
	return nil, nil
}

func (nc *NoopCursor) Seek(seek []byte) (key []byte, value []byte) {
	return nil, nil
}
