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
