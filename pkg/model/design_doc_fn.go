package model

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
	return string(ddfn.Type) + ":" + ddfn.DesignDocID + ":" + ddfn.FnName
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
