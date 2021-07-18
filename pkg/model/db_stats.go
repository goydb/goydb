package model

type DatabaseStats struct {
	FileSize    uint64
	DocCount    uint64
	DocDelCount uint64
	Alloc       uint64
	InUse       uint64
}
