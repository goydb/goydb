package model

import (
	"encoding/json"
	"reflect"
	"slices"
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

	// Full revision chain, newest-first (e.g. ["3-abc","2-def","1-ghi"]).
	// Not exposed in JSON; serialised to BSON storage only.
	RevHistory []string `bson:"rev_history,omitempty" json:"-"`

	// View results (goydb-specific, not unmarshaled from JSON)
	Key   interface{} `json:"-"`
	Value interface{} `json:"-"`

	// Search results (goydb-specific, not unmarshaled from JSON)
	Fields  map[string]interface{}       `json:"-"`
	Options map[string]SearchIndexOption `json:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling for Document.
// It unmarshals known fields into their struct fields and captures all other
// fields into the Data map.
func (doc *Document) UnmarshalJSON(data []byte) error {
	// First unmarshal into a raw map to get all fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Initialize Data if needed
	if doc.Data == nil {
		doc.Data = make(map[string]interface{})
	}

	// Extract known fields and populate Data with everything
	for key, value := range raw {
		switch key {
		case "_id":
			if id, ok := value.(string); ok {
				doc.ID = id
			}
		case "_rev":
			if rev, ok := value.(string); ok {
				doc.Rev = rev
			}
		case "_deleted":
			if deleted, ok := value.(bool); ok {
				doc.Deleted = deleted
			}
		case "_local_seq":
			if seq, ok := value.(float64); ok {
				doc.LocalSeq = uint64(seq)
			}
		case "_attachments":
			// Handle attachments separately
			if attachments, ok := value.(map[string]interface{}); ok {
				doc.Attachments = make(map[string]*Attachment)
				for name, attData := range attachments {
					if attMap, ok := attData.(map[string]interface{}); ok {
						att := &Attachment{}
						if contentType, ok := attMap["content_type"].(string); ok {
							att.ContentType = contentType
						}
						if length, ok := attMap["length"].(float64); ok {
							att.Length = int64(length)
						}
						if stub, ok := attMap["stub"].(bool); ok {
							att.Stub = stub
						}
						if digest, ok := attMap["digest"].(string); ok {
							att.Digest = digest
						}
						if revpos, ok := attMap["revpos"].(float64); ok {
							att.Revpos = int(revpos)
						}
						if encoding, ok := attMap["encoding"].(string); ok {
							att.Encoding = encoding
						}
						doc.Attachments[name] = att
					}
				}
			}
		}
		// Always store in Data for consistent document representation
		doc.Data[key] = value
	}

	return nil
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

// HasRevision reports whether rev is the current revision or any ancestor
// recorded in RevHistory.
func (doc Document) HasRevision(rev string) bool {
	if doc.Rev == rev {
		return true
	}
	return slices.Contains(doc.RevHistory, rev)
}

// compareRevs returns negative/0/positive per CouchDB's rev ordering rule:
// higher generation number wins; ties broken lexicographically by hash.
func compareRevs(a, b string) int {
	aGen, aHash, _ := strings.Cut(a, "-")
	bGen, bHash, _ := strings.Cut(b, "-")
	aN, _ := strconv.Atoi(aGen)
	bN, _ := strconv.Atoi(bGen)
	if aN != bN {
		return aN - bN
	}
	return strings.Compare(aHash, bHash)
}

// WinnerRev returns the winning revision among the supplied rev strings.
func WinnerRev(revs []string) string {
	return slices.MaxFunc(revs, compareRevs)
}

type Revisions struct {
	IDs   []string `json:"ids"`
	Start int64    `json:"start"`
}

func (doc Document) Revisions() Revisions {
	if len(doc.RevHistory) > 0 {
		hashes := make([]string, len(doc.RevHistory))
		for i, r := range doc.RevHistory {
			hashes[i] = strings.SplitN(r, "-", 2)[1]
		}
		start, _ := strconv.ParseInt(strings.SplitN(doc.RevHistory[0], "-", 2)[0], 10, 64)
		return Revisions{IDs: hashes, Start: start}
	}
	// Fallback for documents stored before revision history was introduced.
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
		IDs:   []string{parts[1]},
		Start: i,
	}
}

func (doc Document) NextLocalRevision() string {
	if doc.Rev == "" {
		return "0-1"
	}
	parts := strings.SplitN(doc.Rev, "-", 2)
	if len(parts) == 2 && parts[0] == "0" {
		n, _ := strconv.Atoi(parts[1])
		return "0-" + strconv.Itoa(n+1)
	}
	return "0-1" // migration from content-hash rev
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

	MapFn        string
	ReduceFn     string
	SearchFn     string
	Analyzer     string
	FilterFn     string
	UpdateFnCode string
	MangoFields  []string
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

	// mango indexes
	mangoIndexes, ok := doc.Data["mango_indexes"].(map[string]interface{})
	if ok {
		for name, defI := range mangoIndexes {
			def, ok := defI.(map[string]interface{})
			if !ok {
				continue
			}
			rawFields, _ := def["fields"].([]interface{})
			var fields []string
			for _, f := range rawFields {
				if s, ok := f.(string); ok {
					fields = append(fields, s)
				}
			}
			functions = append(functions, &Function{
				doc:         doc,
				Name:        name,
				Type:        MangoFn,
				MangoFields: fields,
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

// MangoIndex returns the named Mango index from this design document, if it exists.
func (doc *Document) MangoIndex(name string) (*MangoIndex, bool) {
	indexes, ok := doc.Data["mango_indexes"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	defI, ok := indexes[name]
	if !ok {
		return nil, false
	}
	def, ok := defI.(map[string]interface{})
	if !ok {
		return nil, false
	}
	rawFields, _ := def["fields"].([]interface{})
	var fields []string
	for _, f := range rawFields {
		if s, ok := f.(string); ok {
			fields = append(fields, s)
		}
	}
	return &MangoIndex{
		Name:   name,
		Ddoc:   doc.ID,
		Fields: fields,
	}, true
}

// MangoIndexes returns all Mango indexes defined in this design document.
func (doc *Document) MangoIndexes() []*MangoIndex {
	indexes, ok := doc.Data["mango_indexes"].(map[string]interface{})
	if !ok {
		return nil
	}
	var result []*MangoIndex
	for name, defI := range indexes {
		def, ok := defI.(map[string]interface{})
		if !ok {
			continue
		}
		rawFields, _ := def["fields"].([]interface{})
		var fields []string
		for _, f := range rawFields {
			if s, ok := f.(string); ok {
				fields = append(fields, s)
			}
		}
		result = append(result, &MangoIndex{
			Name:   name,
			Ddoc:   doc.ID,
			Fields: fields,
		})
	}
	return result
}

// Filters returns all filter functions defined in this design doc
func (doc *Document) Filters() []*Function {
	if !doc.IsDesignDoc() {
		return nil
	}

	filters, ok := doc.Data["filters"].(map[string]interface{})
	if !ok {
		return nil
	}

	var fns []*Function
	for name, fnCode := range filters {
		if code, ok := fnCode.(string); ok {
			fns = append(fns, &Function{
				doc:      doc,
				Name:     name,
				Type:     FilterFn,
				FilterFn: code,
			})
		}
	}
	return fns
}

// Filter returns a specific filter function by name
func (doc *Document) Filter(name string) (*Function, bool) {
	if !doc.IsDesignDoc() {
		return nil, false
	}

	filters, ok := doc.Data["filters"].(map[string]interface{})
	if !ok {
		return nil, false
	}

	fnCode, ok := filters[name]
	if !ok {
		return nil, false
	}

	code, ok := fnCode.(string)
	if !ok {
		return nil, false
	}

	return &Function{
		doc:      doc,
		Name:     name,
		Type:     FilterFn,
		FilterFn: code,
	}, true
}

// Updates returns all update functions defined in this design doc
func (doc *Document) Updates() []*Function {
	if !doc.IsDesignDoc() {
		return nil
	}

	updates, ok := doc.Data["updates"].(map[string]interface{})
	if !ok {
		return nil
	}

	var fns []*Function
	for name, fnCode := range updates {
		if code, ok := fnCode.(string); ok {
			fns = append(fns, &Function{
				doc:          doc,
				Name:         name,
				Type:         UpdateFn,
				UpdateFnCode: code,
			})
		}
	}
	return fns
}

// Update returns a specific update function by name
func (doc *Document) Update(name string) (*Function, bool) {
	if !doc.IsDesignDoc() {
		return nil, false
	}

	updates, ok := doc.Data["updates"].(map[string]interface{})
	if !ok {
		return nil, false
	}

	fnCode, ok := updates[name]
	if !ok {
		return nil, false
	}

	code, ok := fnCode.(string)
	if !ok {
		return nil, false
	}

	return &Function{
		doc:          doc,
		Name:         name,
		Type:         UpdateFn,
		UpdateFnCode: code,
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
