package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// DocumentField provide a field from a document
// handles nested document traversal
type DocumentField interface {
	// Field get a field value (nesting supported with . operator)
	Field(path string) interface{}
	Exists(path string) bool
}

// SelectorQuery matches a document or doesn't
type SelectorQuery interface {
	// Match returns true if the selector matched
	// the passed otherwise false
	Match(df DocumentField) (bool, error)
	String() string
}

// SortQuery allows sorting of documents
type SortQuery interface {
	// Less returns true if l is lower then r
	Less(l, r *Document) bool
}

type FindQuery struct {
	// Selector JSON object describing criteria used to select documents.
	// More information provided in the section on selector syntax.
	Selector SelectorGroup `json:"selector"`
	// Sort JSON array following sort syntax. Optional
	Sort SortList `json:"sort"`
	// Fields JSON array specifying which fields of each object should be returned.
	// If it is omitted, the entire object is returned. More information
	// 	provided in the section on filtering fields. Optional
	Fields []string `json:"fields"`
	// Limit Maximum number of results returned. Default is 25. Optional
	Limit int `json:"limit"`
	// Skip the first ‘n’ results, where ‘n’ is the value specified. Optional
	Skip int `json:"skip"`
	// Bookmark a string that enables you to specify which page of
	// results you require. Used for paging through result sets.
	// Every query returns an opaque string under the bookmark key
	// that can then be passed back in a query to get the next page
	// of results. If any part of the selector query changes between
	// requests, the results are undefined.
	Bookmark string `json:"bookmark"`
	// Include execution statistics in the query response.
	ExecutionStats bool `json:"execution_stats"`
}

func (fq FindQuery) Match(doc *Document) (bool, error) {
	return fq.Selector.Match(doc)
}

func (fq FindQuery) SortDocuments(docs []*Document) {
	if len(fq.Sort) == 0 {
		return // no sort parameters given, leave orginal order
	}
	sort.SliceStable(docs, func(i, j int) bool {
		return fq.Sort.Less(docs[i], docs[j])
	})
}

type ExecutionStats struct {
	// TotalKeysExamined
	// Number of index keys examined. Currently always 0.
	TotalKeysExamined int `json:"total_keys_examined"`

	// TotalDocsExamined
	// Number of documents fetched from the database / index,
	// equivalent to using include_docs=true in a view. These
	// may then be filtered in-memory to further narrow down
	// the result set based on the selector.
	TotalDocsExamined int `json:"total_docs_examined"`

	// TotalQuorumDocsExamined
	// Number of documents fetched from the database using an
	// out-of-band document fetch. This is only non-zero when
	// read quorum > 1 is specified in the query parameters.
	TotalQuorumDocsExamined int `json:"total_quorum_docs_examined"`

	// ResultsReturned
	// Number of results returned from the query. Ideally this
	// should not be significantly lower than the total
	// documents / keys examined.
	ResultsReturned int `json:"results_returned"`

	// ExecutionTime
	// Total execution time in milliseconds as measured by the
	// database.
	ExecutionTime float64 `json:"execution_time_ms"`
}

type SortList []Sort

func (sl *SortList) UnmarshalJSON([]byte) error {
	// TODO implement sort unmarshal
	return nil
}

func (sl SortList) Less(l, r *Document) bool {
	var ltr = true
	for _, sq := range sl {
		ltr = ltr && sq.Less(l, r)
	}
	return ltr
}

const (
	SortOrderAsc  = false
	SortOrderDesc = true
)

type Sort struct {
	Field string
	Order bool
}

func (s Sort) Less(l, r *Document) bool {
	// TODO: implement
	return false
}

type SelectorGroupOp string

const (
	SelectorAnd         SelectorGroupOp = "$and"         // Matches if all the selectors in the array match.
	SelectorOr          SelectorGroupOp = "$or"          // Matches if any of the selectors in the array match. All selectors must use the same index.
	SelectorNot         SelectorGroupOp = "$not"         // Matches if the given selector does not match.
	SelectorNor         SelectorGroupOp = "$nor"         // Matches if none of the selectors in the array match.
	SelectorElemMatch   SelectorGroupOp = "$elemMatch"   // Matches and returns all documents that contain an array field with at least one element that matches all the specified query criteria.
	SelectorAllMatch    SelectorGroupOp = "$allMatch"    // Matches and returns all documents that contain an array field with all its elements matching all the specified query criteria.
	SelectorKeyMapMatch SelectorGroupOp = "$keyMapMatch" // Matches and returns all documents that contain a map that contains at least one key that matches all the specified query criteria.
)

var groupSelectors = map[string]bool{
	string(SelectorAnd):         true,
	string(SelectorOr):          true,
	string(SelectorNot):         true,
	string(SelectorNor):         true,
	string(SelectorAll):         false,
	string(SelectorElemMatch):   true,
	string(SelectorAllMatch):    true,
	string(SelectorKeyMapMatch): true,
	string(SelectorOpLt):        false,
	string(SelectorOpLte):       false,
	string(SelectorOpEq):        false,
	string(SelectorOpNe):        false,
	string(SelectorOpGte):       false,
	string(SelectorOpGt):        false,
	string(SelectorOpExists):    false,
	string(SelectorOpType):      false,
	string(SelectorOpIn):        false,
	string(SelectorOpNin):       false,
	string(SelectorOpSize):      false,
	string(SelectorOpMod):       false,
	string(SelectorOpRegex):     false,
}

type SelectorGroup struct {
	Members   []SelectorQuery
	Operation SelectorGroupOp
}

func (sg SelectorGroup) String() string {
	members := make([]string, len(sg.Members))
	for i, m := range sg.Members {
		members[i] = m.String()
	}

	return fmt.Sprintf("%s(%v)", string(sg.Operation), strings.Join(members, ", "))
}

func (sg *SelectorGroup) UnmarshalJSON(blob []byte) error {
	if sg.Operation == "" { // set default
		sg.Operation = SelectorAnd
	}

	// extract first layer
	var data map[string]json.RawMessage
	err := json.Unmarshal(blob, &data)
	if err != nil {
		return err
	}

	for name, raw := range data {
		isGroupSelector := groupSelectors[name]

		if isGroupSelector {
			// we expect an array, for the new selector group
			sg1 := SelectorGroup{
				Operation: SelectorGroupOp(name),
			}

			// unmarshal array
			var dataArray []map[string]json.RawMessage
			err := json.Unmarshal(raw, &dataArray)
			if err != nil {
				return err
			}

			// handle array
			for _, da := range dataArray {
				// each element of the array is either a group
				// or an field selector
				for key, value := range da {
					groupSelectorSub, ok := groupSelectors[key]

					// it is a field selector
					if !ok {
						// single selector
						if len(da) == 1 {
							fs := FieldSelector{
								Field: key,
							}
							err := json.Unmarshal(value, &fs)
							if err != nil {
								return err
							}
							sg1.Members = append(sg1.Members, &fs)
						} else { // multi selector
							return fmt.Errorf("not implemented: multi selector %q", key)
						}

					} else if groupSelectorSub {
						if len(da) > 1 {
							return fmt.Errorf("with a group selector only one element can be given, to many arugments for %v", name)
						}

						sg2 := SelectorGroup{
							Operation: SelectorGroupOp(key),
						}
						err := json.Unmarshal(value, &sg2)
						if err != nil {
							return err
						}
						sg1.Members = append(sg1.Members, &sg2)
					} else {
						fs := FieldSelector{
							Field: key,
						}
						err := json.Unmarshal(value, &fs)
						if err != nil {
							return err
						}
						sg1.Members = append(sg1.Members, &fs)
					}
				}
			}
			sg.Members = append(sg.Members, &sg1)
		} else {
			fs := FieldSelector{
				Field: name,
			}

		pathing:
			for {
				if raw[0] == '{' {
					var data map[string]json.RawMessage
					err := json.Unmarshal(raw, &data)
					if err != nil {
						return err
					}
					for key, value := range data {
						_, isSelector := groupSelectors[key]
						if isSelector { // backtrack
							break pathing
						}

						fs.Field += "." + key
						raw = value
						break pathing
					}
				} else {
					break pathing
				}
			}
			err := json.Unmarshal(raw, &fs)
			if err != nil {
				return err
			}

			sg.Members = append(sg.Members, &fs)
		}
	}

	return nil
}

func (sg SelectorGroup) Match(df DocumentField) (bool, error) {
	// an empty list is always false
	if len(sg.Members) == 0 {
		return false, nil
	}

	switch sg.Operation {
	case SelectorAnd:
		for _, m := range sg.Members {
			ok, err := m.Match(df)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil // all match
	case SelectorOr:
		for _, m := range sg.Members {
			ok, err := m.Match(df)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil // one match
			}
		}
		return false, nil // no match
	case SelectorNot:
		ok, err := sg.Members[0].Match(df)
		return !ok, err
	case SelectorNor:
		for _, m := range sg.Members {
			ok, err := m.Match(df)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		}
		return true, nil // no match
	default:
		panic(fmt.Errorf("undefined operation: %q", sg.Operation))
	}
}

type SelectorOp string

const (
	SelectorOpLt     SelectorOp = "$lt"     // The field is less than the argument
	SelectorOpLte    SelectorOp = "$lte"    // The field is less than or equal to the argument.
	SelectorOpEq     SelectorOp = "$eq"     // The field is equal to the argument
	SelectorOpNe     SelectorOp = "$ne"     // The field is not equal to the argument.
	SelectorOpGte    SelectorOp = "$gte"    // The field is greater than or equal to the argument.
	SelectorOpGt     SelectorOp = "$gt"     // The field is greater than the to the argument.
	SelectorOpExists SelectorOp = "$exists" // Check whether the field exists or not, regardless of its value.
	SelectorOpType   SelectorOp = "$type"   // Check the document field’s type. Valid values are "null", "boolean", "number", "string", "array", and "object".
	SelectorOpIn     SelectorOp = "$in"     // The document field must exist in the list provided.
	SelectorOpNin    SelectorOp = "$nin"    // The document field not must exist in the list provided.
	SelectorOpSize   SelectorOp = "$size"   // Special condition to match the length of an array field in a document. Non-array fields cannot match this condition.
	SelectorOpMod    SelectorOp = "$mod"    // Divisor and Remainder are both positive or negative integers. Non-integer values result in a 404. Matches documents where field % Divisor == Remainder is true, and only when the document field is an integer.
	SelectorOpRegex  SelectorOp = "$regex"  // A regular expression pattern to match against the document field. Only matches when the field is a string value and matches the supplied regular expression. The matching algorithms are based on the Perl Compatible Regular Expression (PCRE) library. For more information about what is implemented, see the see the https://golang.org/pkg/regexp/

	SelectorAll SelectorOp = "$all" // Matches an array value if it contains all the elements of the argument array.
)

type FieldSelector struct {
	Field     string
	Value     interface{}
	Operation SelectorOp
}

func (fs *FieldSelector) UnmarshalJSON(blob []byte) error {
	fs.Operation = SelectorOpEq // default

	// direct value
	if blob[0] != '{' {
		return json.Unmarshal(blob, &fs.Value)
	}

	// selector definition
	var def map[string]interface{}
	err := json.Unmarshal(blob, &def)
	if err != nil {
		return err
	}

	if len(def) != 1 {
		return fmt.Errorf("invalid field selector %q, only one selector is allowed", string(blob))
	}

	for key, value := range def {
		fs.Operation = SelectorOp(key)
		fs.Value = value
	}

	return nil
}

func (fs FieldSelector) String() string {
	return fmt.Sprintf("%s(%v, %v)", string(fs.Operation), fs.Field, fs.Value)
}

func (fs FieldSelector) Match(df DocumentField) (bool, error) {
	var svField, svValue SelectorValue
	svField.Set(df.Field(fs.Field))
	svValue.Set(fs.Value)

	switch fs.Operation {
	case SelectorOpLt:
		return svField.LessThen(&svValue), nil
	case SelectorOpLte:
		ok, err := svField.Match(&svValue)
		if err != nil {
			return false, err
		}
		return ok || svField.LessThen(&svValue), nil
	case SelectorOpEq:
		return svField.Match(&svValue)
	case SelectorOpNe:
		ok, err := svField.Match(&svValue)
		if err != nil {
			return false, err
		}
		return !ok, nil
	case SelectorOpGte:
		ok, err := svField.Match(&svValue)
		if err != nil {
			return false, err
		}
		return ok || svField.GeaterThen(&svValue), nil
	case SelectorOpGt:
		return svField.GeaterThen(&svValue), nil
	case SelectorOpExists:
		return df.Exists(fs.Field), nil
	case SelectorOpType:
		if svValue.t == SelectorValueTypeString {
			return svField.Type() == svValue.s, nil
		} else {
			return false, fmt.Errorf("value has to be of type string")
		}
	case SelectorOpIn:
		for _, v := range svValue.Values() {
			ok, err := svField.Match(&v)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case SelectorOpNin:
		for _, v := range svValue.Values() {
			ok, err := svField.Match(&v)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		}
		return true, nil
	case SelectorOpSize:
		if !svValue.IsNumber() {
			return false, fmt.Errorf("value has to be of type number")
		}
		return svField.ArrayLen() == int(svValue.f)+int(svValue.i), nil
	case SelectorOpMod:
		if svField.t != SelectorValueTypeInt {
			return false, nil
		}
		if svValue.ArrayLen() != 2 {
			return false, fmt.Errorf("value has to be an array of two numbers")
		}
		arr := svValue.Values()
		if arr[0].t != SelectorValueTypeInt && arr[1].t != SelectorValueTypeInt {
			return false, fmt.Errorf("only numbers are allowed")
		}
		if arr[0].i == 0 {
			return false, fmt.Errorf("integer divide by zero")
		}
		return svField.i%arr[0].i == arr[1].i, nil
	case SelectorOpRegex:
		// regex has to be string
		if svValue.t != SelectorValueTypeString {
			return false, fmt.Errorf("value has to be of type string")
		}
		// value has to be string
		if svField.t != SelectorValueTypeString {
			return false, fmt.Errorf("field has to be of type string")
		}
		// compile regex if not done already
		if svValue.o == nil {
			re, err := regexp.Compile(svValue.s)
			if err != nil {
				return false, fmt.Errorf("Failed to compile regex: %v", err)
			}
			svValue.o = re
		}
		// match regex
		re := svValue.o.(*regexp.Regexp)
		return re.MatchString(svField.s), nil
	case SelectorAll:
		// ensure both values are arrays
		if svField.ArrayLen() <= 0 {
			return false, fmt.Errorf("field is no array")
		}
		if svValue.ArrayLen() <= 0 {
			return false, fmt.Errorf("value is no array")
		}

		// compare values
		svFieldValues := svField.Values()
		svValues := svValue.Values()

		found := 0
	loop:
		for _, vv := range svValues {
			for _, fv := range svFieldValues {
				ok, err := vv.Match(&fv)
				if err != nil {
					return false, err
				}
				if ok {
					found++
					continue loop
				}
			}

			// stop searching after first miss
			return false, nil
		}
		return found == len(svValues), nil
	default:
		return false, fmt.Errorf("undefined operation: %q", fs.Operation)
	}
}

type SelectorValueType int

const (
	SelectorValueTypeNil = iota
	SelectorValueTypeBool
	SelectorValueTypeArray
	SelectorValueTypeObject
	SelectorValueTypeInt
	SelectorValueTypeFloat
	SelectorValueTypeString
	SelectorValueTypeOther
)

type SelectorValue struct {
	t SelectorValueType
	i int64
	f float64
	s string
	o interface{}
}

func (sv SelectorValue) Values() []SelectorValue {
	v := reflect.ValueOf(sv.o)
	if v.Kind() != reflect.Slice {
		return nil
	}
	arr := make([]SelectorValue, v.Len())
	for i := 0; i < v.Len(); i++ {
		av := v.Index(i)
		arr[i].Set(av.Interface())
	}
	return arr
}

func (sv SelectorValue) ArrayLen() int {
	v := reflect.ValueOf(sv.o)
	if v.Kind() != reflect.Slice {
		return -1
	}
	return v.Len()
}

func (sv *SelectorValue) Set(v interface{}) {
	switch ft := v.(type) {
	case nil:
		sv.t = SelectorValueTypeNil
	case bool:
		sv.t = SelectorValueTypeBool
		if ft {
			sv.i = 1
		}
	case int:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case int8:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case int16:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case int32:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case int64:
		sv.t = SelectorValueTypeInt
		sv.i = ft
	case uint:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case uint8:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case uint16:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case uint32:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case uint64:
		sv.t = SelectorValueTypeInt
		sv.i = int64(ft)
	case float32:
		sv.t = SelectorValueTypeFloat
		sv.f = float64(ft)
	case float64:
		sv.t = SelectorValueTypeFloat
		sv.f = ft
	case string:
		sv.t = SelectorValueTypeString
		sv.s = ft
	case []byte:
		sv.t = SelectorValueTypeString
		sv.s = string(ft)
	default:
		switch reflect.TypeOf(v).Kind() {
		case reflect.Array, reflect.Slice:
			sv.t = SelectorValueTypeArray
		case reflect.Struct, reflect.Map:
			sv.t = SelectorValueTypeObject
		default:
			sv.t = SelectorValueTypeOther
		}
		sv.o = v
	}
}

func (sv *SelectorValue) Match(other *SelectorValue) (bool, error) {
	// assure the types match
	if sv.t != other.t {
		if sv.IsNumber() && other.IsNumber() {
			return sv.f+float64(sv.i) == other.f+float64(other.i), nil
		}
		return false, nil
	}
	switch sv.t {
	case SelectorValueTypeNil:
		return true, nil // both have the same type already, so both are nil
	case SelectorValueTypeInt, SelectorValueTypeBool:
		return sv.i == other.i, nil
	case SelectorValueTypeFloat:
		return sv.f == other.f, nil
	case SelectorValueTypeString:
		return sv.s == other.s, nil
	case SelectorValueTypeOther, SelectorValueTypeArray, SelectorValueTypeObject:
		return reflect.DeepEqual(sv.o, other.o), nil
	default:
		return false, errors.New("unkown selector value type")
	}
}

func (sv *SelectorValue) LessThen(other *SelectorValue) bool {
	// assure the types match
	if sv.t != other.t {
		if sv.IsNumber() && other.IsNumber() {
			return sv.f+float64(sv.i) < other.f+float64(other.i)
		}
		return false
	}
	switch sv.t {
	case SelectorValueTypeInt:
		return sv.i < other.i
	case SelectorValueTypeFloat:
		return sv.f < other.f
	case SelectorValueTypeString:
		return strings.Compare(sv.s, other.s) == -1
	case SelectorValueTypeOther,
		SelectorValueTypeBool,
		SelectorValueTypeArray,
		SelectorValueTypeObject,
		SelectorValueTypeNil:
		return false
	default:
		panic("unkown selector value type")
	}
}

func (sv *SelectorValue) GeaterThen(other *SelectorValue) bool {
	// assure the types match
	if sv.t != other.t {
		if sv.IsNumber() && other.IsNumber() {
			return sv.f+float64(sv.i) > other.f+float64(other.i)
		}
		return false
	}
	switch sv.t {
	case SelectorValueTypeInt:
		return sv.i > other.i
	case SelectorValueTypeFloat:
		return sv.f > other.f
	case SelectorValueTypeString:
		return strings.Compare(sv.s, other.s) == 1
	case SelectorValueTypeOther,
		SelectorValueTypeBool,
		SelectorValueTypeArray,
		SelectorValueTypeObject,
		SelectorValueTypeNil:
		return false
	default:
		panic("unkown selector value type")
	}
}

func (sv *SelectorValue) IsNumber() bool {
	return sv.t == SelectorValueTypeFloat || sv.t == SelectorValueTypeInt
}

func (sv *SelectorValue) Type() string {
	switch sv.t {
	case SelectorValueTypeInt, SelectorValueTypeFloat:
		return "number"
	case SelectorValueTypeString:
		return "string"
	case SelectorValueTypeOther, SelectorValueTypeObject:
		return "object"
	case SelectorValueTypeBool:
		return "boolean"
	case SelectorValueTypeArray:
		return "array"
	case SelectorValueTypeNil:
		return "null"
	default:
		panic("unkown selector value type")
	}
}
