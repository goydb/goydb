package model

// IndexStats
//
// Since an index may have multiple records pointing to the same document
// or may ignore documents, the number of Records may be higher than the
// number of Documents.
type IndexStats struct {
	// Documents number of document in the index
	Documents uint64
	// Used number of bytes used by the index
	Used uint64
	// Allocated number of bytes allocated by the index
	Allocated uint64
}
