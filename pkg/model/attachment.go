package model

import "io"

type Attachment struct {
	Filename    string `bson:"-" json:"-"`
	ContentType string `json:"content_type"`
	Revpos      int    `json:"revpos"`
	Digest      string `json:"digest"`
	Length      int64  `json:"length"`
	Stub        bool   `json:"stub,omitempty"`

	// Data holds the inline base64-encoded attachment content.
	// Used during replication to carry attachment data without a separate request.
	// Never stored in BSON; omitted from JSON when empty.
	Data string `json:"data,omitempty" bson:"-"`

	// Encoding holds the content-encoding used by CouchDB for compressed storage
	// (e.g. "gzip"). Populated during replication; cleared after decompression.
	Encoding string `json:"encoding,omitempty" bson:"-"`

	Reader      io.ReadCloser `bson:"-" json:"-"`
	ExpectedRev string        `bson:"-" json:"-"` // revision the client expects; empty = no check
}
