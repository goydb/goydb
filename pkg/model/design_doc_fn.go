package model

import "strings"

type FnType string

const (
	ViewFn   FnType = "views"
	SearchFn FnType = "indexes"
)

type DesignDocFn struct {
	Type        FnType
	DesignDocID string
	FnName      string
}

func (ddfn DesignDocFn) String() string {
	docName := strings.TrimPrefix(ddfn.DesignDocID, string(DesignDocPrefix))
	return string(ddfn.Type) + ":" + docName + ":" + ddfn.FnName
}

func (ddfn DesignDocFn) Bucket() []byte {
	return []byte(ddfn.String())
}

func NewSearchFn(designDocID, fnName string) DesignDocFn {
	return DesignDocFn{
		Type:        SearchFn,
		DesignDocID: designDocID,
		FnName:      fnName,
	}
}

func NewViewFn(designDocID, fnName string) DesignDocFn {
	return DesignDocFn{
		Type:        ViewFn,
		DesignDocID: designDocID,
		FnName:      fnName,
	}
}
