package model

import (
	"encoding/binary"
	"reflect"
	"strconv"
	"strings"
)

type Document struct {
	ID          string                 `json:"_id,omitempty"`
	Rev         string                 `json:"_rev,omitempty"`
	Deleted     bool                   `json:"_deleted,omitempty"`
	LocalSeq    uint64                 `json:"_local_seq,omitempty"`
	Attachments map[string]*Attachment `json:"_attachments,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Key         interface{}            `json:"key,omitempty"`
	Value       interface{}            `json:"value,omitempty"`
}

func (doc Document) ValidUpdateRevision(newDoc *Document) bool {
	oldRev, ok := doc.Revision()
	if ok {
		newRev, ok := newDoc.Revision()
		if !ok || newRev != oldRev {
			// update without correct rev forbidden if
			// document already exists
			return false
		}
	}
	return true
}

func (doc Document) Revision() (string, bool) {
	if doc.Rev != "" {
		return doc.Rev, true
	}
	rev, ok := doc.Data["_rev"].(string)
	return rev, ok
}

type Revisions struct {
	IDs   []string `json:"ids"`
	Start int64    `json:"start"`
}

func (doc Document) Revisions() Revisions {
	rev, ok := doc.Revision()
	if !ok {
		panic("no revision")
	}
	parts := strings.Split(rev, "-")
	i, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		panic("invalid revision")
	}
	return Revisions{
		IDs: []string{
			parts[1],
		},
		Start: i,
	}
}

func (doc Document) NextSequence() int {
	rev, ok := doc.Revision()
	if !ok {
		return 1
	}

	i := strings.Index(rev, "-")
	val, err := strconv.ParseInt(rev[:i], 10, 64)
	if err != nil {
		return 1 // this should never happen, but if so fallback to 0
	}
	return int(val) + 1
}

func FormatLocalSeq(seq uint64) string {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, seq)
	return string(b)
}

func (doc Document) FormatLocalSeq() string {
	return FormatLocalSeq(doc.LocalSeq)
}

func (doc Document) Language() string {
	v, ok := doc.Data["language"].(string)
	if ok {
		return v
	}
	return "" // default
}

func (doc Document) IsDesignDoc() bool {
	return strings.HasPrefix(doc.ID, "_design/")
}

func (doc Document) IsLocalDoc() bool {
	return strings.HasPrefix(doc.ID, "_local/")
}

type ViewFunctions struct {
	Name     string
	MapFn    string
	ReduceFn string
}

func (doc Document) ViewFunctions() []*ViewFunctions {
	var vfn []*ViewFunctions

	views, ok := doc.Data["views"].(map[string]interface{})
	if !ok {
		return nil
	}

	for name, viewInterface := range views {
		view, ok := viewInterface.(map[string]interface{})
		if !ok {
			continue
		}

		mapFn, _ := view["map"].(string)
		reduceFn, _ := view["reduce"].(string)

		vfn = append(vfn, &ViewFunctions{
			Name:     name,
			MapFn:    mapFn,
			ReduceFn: reduceFn,
		})
	}

	return vfn
}

func (doc *Document) Field(path string) interface{} {
	parts := strings.Split(path, ".")
	v := reflect.ValueOf(doc.Data)
	if v.IsZero() {
		return nil
	}

	// walk the path
	for _, part := range parts {
		// not a map return nil
		if v.Kind() != reflect.Map {
			return nil
		}

		// get value of the path
		key := reflect.ValueOf(part)
		if key.IsZero() {
			return nil
		}

		value := v.MapIndex(key)
		if !value.IsValid() || value.IsZero() {
			return nil
		} else {
			v = reflect.ValueOf(value.Interface())
		}
	}

	return v.Interface()
}

func (doc *Document) Exists(path string) bool {
	return doc.Field(path) != nil
}
