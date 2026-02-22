package model

var DocsBucket = []byte("docs")

// DocLeavesBucket stores all concurrent leaf revisions for a document
// (the winner plus any conflicting branches).
// Key: <docID> ++ \x00 ++ <revID>
var DocLeavesBucket = []byte("doc_leaves")

// AttRefsBucket holds reference counts for content-addressed attachment blobs.
// Key: MD5 hex digest (32 chars) or "_scheme" sentinel.
// Value: little-endian int64 reference count.
var AttRefsBucket = []byte("att_refs")

// MetaBucket stores per-database key/value metadata (e.g. revs_limit).
var MetaBucket = []byte("meta")

// RevsLimitKey is the key for the revs_limit value in MetaBucket.
// Value is a big-endian uint64. Default is 1000 when absent.
var RevsLimitKey = []byte("revs_limit")
