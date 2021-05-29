package model

// IndexStats
//
// Since an index may have multiple records pointing to the same document
// or may ignore documents, the number of Records may be higher than the
// number of Documents.
type IndexStats struct {
	// Documents number of document in the index
	Documents uint64
	// Size number of bytes used by the index
	Size uint64
	// Number of records (keys)
	Records uint64
}
