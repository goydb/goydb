package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ViewKeyString returns a canonical JSON string for key k, usable as a Go map key.
func ViewKeyString(k interface{}) string {
	if k == nil {
		return "null"
	}
	b, err := json.Marshal(k)
	if err != nil {
		return fmt.Sprintf("%v", k)
	}
	return string(b)
}

// ViewKeyCmp compares two CouchDB view-key values using CouchDB's collation order:
//
//	null < false < true < numbers < strings < arrays < objects
//
// Returns -1, 0, or 1.
func ViewKeyCmp(a, b interface{}) int {
	ta, tb := viewKeyTypePriority(a), viewKeyTypePriority(b)
	if ta != tb {
		if ta < tb {
			return -1
		}
		return 1
	}
	switch ta {
	case 0: // null == null
		return 0
	case 1: // bool
		ab, bb := viewToBool(a), viewToBool(b)
		if ab == bb {
			return 0
		}
		if !ab {
			return -1
		}
		return 1
	case 2: // number — compare as float64
		af, bf := viewToFloat64(a), viewToFloat64(b)
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	case 3: // string — lexicographic
		return strings.Compare(a.(string), b.(string))
	case 4: // array — element by element, shorter first
		aa := viewToSlice(a)
		ba := viewToSlice(b)
		for i := 0; i < len(aa) && i < len(ba); i++ {
			if c := ViewKeyCmp(aa[i], ba[i]); c != 0 {
				return c
			}
		}
		if len(aa) < len(ba) {
			return -1
		}
		if len(aa) > len(ba) {
			return 1
		}
		return 0
	default: // object / unknown — treated as equal
		return 0
	}
}

func viewKeyTypePriority(v interface{}) int {
	if v == nil {
		return 0
	}
	switch v.(type) {
	case bool:
		return 1
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return 2
	case string:
		return 3
	case []interface{}:
		return 4
	default:
		return 5 // map / object
	}
}

func viewToBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

func viewToFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float32:
		return float64(n)
	case float64:
		return n
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case uint8:
		return float64(n)
	case uint16:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return 0
}

func viewToSlice(v interface{}) []interface{} {
	s, _ := v.([]interface{})
	return s
}
