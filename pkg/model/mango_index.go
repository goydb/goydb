package model

// MangoIndex is the domain model for a Mango (_find) index,
// parallel to model.View.
type MangoIndex struct {
	Name   string
	Ddoc   string // "_design/<ddoc>"
	Fields []string
}

// DesignDocFn returns the DesignDocFn that identifies this index.
func (mi *MangoIndex) DesignDocFn() *DesignDocFn {
	return &DesignDocFn{Type: MangoFn, DesignDocID: mi.Ddoc, FnName: mi.Name}
}
