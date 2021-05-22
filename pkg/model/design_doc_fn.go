package model

import (
	"fmt"
	"strings"
)

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

func ParseDesignDocFn(str string) (*DesignDocFn, error) {
	parts := strings.Split(str, ":")

	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid design doc fn %q, expected 3 got %d parts", str, len(parts))
	}

	return &DesignDocFn{
		Type:        FnType(parts[0]),
		DesignDocID: parts[1],
		FnName:      parts[2],
	}, nil
}
