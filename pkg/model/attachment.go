package model

import "io"

type Attachment struct {
	Filename    string `bson:"-" json:"-"`
	ContentType string `json:"content_type"`
	Revpos      int    `json:"revpos"`
	Digest      string `json:"digest"`
	Length      int64  `json:"length"`
	Stub        bool   `json:"stub"`

	Reader io.ReadCloser `bson:"-" json:"-"`
}
