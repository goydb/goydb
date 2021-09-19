package model

import (
	"reflect"
	"strconv"
	"strings"
)

type DocPrefix string

var (
	DesignDocPrefix DocPrefix = "_design/"
	LocalDocPrefix  DocPrefix = "_local/"
)

type Document struct {
	// Meta
	ID       string `json:"_id,omitempty"`
	Rev      string `json:"_rev,omitempty"`
	Deleted  bool   `json:"_deleted,omitempty"`
	LocalSeq uint64 `json:"_local_seq,omitempty"`

	// Data
	Attachments map[string]*Attachment `json:"_attachments,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`

	// View
	Key   interface{} `json:"key,omitempty"`
	Value interface{} `json:"value,omitempty"`

	// Search
	Fields  map[string]interface{}       `json:"fields,omitempty"`
	Options map[string]SearchIndexOption `json:"options,omitempty"`
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

func (doc Document) NextSequenceRevision() int {
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

func (doc Document) Language() string {
	v, ok := doc.Data["language"].(string)
	if ok {
		return v
	}
	return "" // default
}

func (doc Document) IsDesignDoc() bool {
	return strings.HasPrefix(doc.ID, string(DesignDocPrefix))
}

func (doc Document) IsLocalDoc() bool {
	return strings.HasPrefix(doc.ID, string(LocalDocPrefix))
}

type Function struct {
	doc *Document

	Name string
	Type FnType

	MapFn    string
	ReduceFn string
	SearchFn string
	Analyzer string
}

func (f *Function) DesignDocFn() *DesignDocFn {
	return &DesignDocFn{
		Type:        f.Type,
		DesignDocID: f.doc.ID,
		FnName:      f.Name,
	}
}

func (doc *Document) Functions() []*Function {
	var functions []*Function

	// regular view functions
	views, ok := doc.Data["views"].(map[string]interface{})
	if ok {
		for name, viewInterface := range views {
			view, ok := viewInterface.(map[string]interface{})
			if !ok {
				continue
			}

			mapFn, _ := view["map"].(string)
			reduceFn, _ := view["reduce"].(string)

			functions = append(functions, &Function{
				doc:      doc,
				Name:     name,
				Type:     ViewFn,
				MapFn:    mapFn,
				ReduceFn: reduceFn,
			})
		}
	}

	// search functions
	indexes, ok := doc.Data["indexes"].(map[string]interface{})
	if ok {
		for name, searchInterface := range indexes {
			search, ok := searchInterface.(map[string]interface{})
			if !ok {
				continue
			}

			SearchMapFn, _ := search["index"].(string)
			Analyzer, _ := search["analyzer"].(string)

			functions = append(functions, &Function{
				doc:      doc,
				Name:     name,
				Type:     SearchFn,
				SearchFn: SearchMapFn,
				Analyzer: Analyzer,
			})
		}
	}

	return functions
}

func (doc *Document) View(name string) (view *View, ok bool) {
	views, ok := doc.Data["views"].(map[string]interface{})
	if !ok {
		return nil, false
	}

	viewInterface, ok := views[name]
	if !ok {
		return nil, false
	}

	viewData, ok := viewInterface.(map[string]interface{})
	if !ok {
		return nil, false
	}

	mapFn, _ := viewData["map"].(string)
	reduceFn, _ := viewData["reduce"].(string)

	return &View{
		Language: doc.Language(),
		MapFn:    mapFn,
		ReduceFn: reduceFn,
	}, true
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
