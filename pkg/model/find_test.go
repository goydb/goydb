package model

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/d5/tengo/v2/require"
	"github.com/stretchr/testify/assert"
)

func TestFind(t *testing.T) {
	QueryTest(t, `{
		"selector": {
		  "$and": [
			{ "title": "Total Recall" },
			{ "year": { "$in": [1984, 1991] } }
		  ]
		}
	}`, map[string]bool{
		`{
			"year":  1984,
			"title": "Total Recall"
		}`: true,
		`{
			"year":  1988,
			"title": "Total Recall 2"
		}`: false,
	})
}

func TestFindSimpleEq(t *testing.T) {
	QueryTest(t, `{
		"selector": {
			"year": { "$eq": 2001 }
		}
	}`, map[string]bool{
		`{"year": 2001}`: true,
		`{"year": 1988}`: false,
	})
}

func TestFindSimplest(t *testing.T) {
	QueryTest(t, `{
		"selector": { "year": 2001 }
	}`, map[string]bool{

		`{"year": 2001}`: true,
		`{"year": 1988}`: false,
	})
}

func TestFindOr(t *testing.T) {
	QueryTest(t, `{
		"selector": {
			"year": 1977,
			"$or": [
				{ "director": "George Lucas" },
				{ "director": "Steven Spielberg" }
			]
		}
	}`, map[string]bool{
		`{"director": "George Lucas","year": 1977}`:     true,
		`{"director": "Steven Spielberg","year": 1977}`: true,
		`{"director": "Michael Giten","year": 1977}`:    false,
	})
}

func TestFindNot(t *testing.T) {
	QueryTest(t, `{
		"selector": {
			"$and": [
				{ "year": { "$gte": 1900 } },
				{ "year": { "$lte": 1903 } },
				{ "$not": { "year": 1901 } }
			]
		}
	}`, map[string]bool{
		`{"year": 1899}`: false,
		`{"year": 1900}`: true,
		`{"year": 1901}`: false,
		`{"year": 1902}`: true,
		`{"year": 1903}`: true,
		`{"year": 1904}`: false,
	})
}

func TestFindNor(t *testing.T) {
	QueryTest(t, `{
		"selector": {
			"$and": [
				{ "year": { "$gte": 1900 } },
				{ "year": { "$lte": 1910 } }
			],
			"$nor": [
				{ "year": 1901 },
				{ "year": 1905 },
				{ "year": 1907 }
			]
		}
	}`, map[string]bool{
		`{"year": 1899}`: false,
		`{"year": 1900}`: true,
		`{"year": 1901}`: false,
		`{"year": 1902}`: true,
		`{"year": 1903}`: true,
		`{"year": 1904}`: true,
		`{"year": 1905}`: false,
		`{"year": 1906}`: true,
		`{"year": 1907}`: false,
		`{"year": 1908}`: true,
		`{"year": 1909}`: true,
		`{"year": 1910}`: true,
		`{"year": 1911}`: false,
	})
}

func TestFindAll(t *testing.T) {
	QueryTest(t, `{
		"selector": {
			"genre": {
				"$all": ["Comedy","Short"]
			}
		}
	}`, map[string]bool{
		`{"genre": ["Drama","Comedy","Short"]}`: true,
		`{"genre": ["Drama","Commedy"]}`:        false,
		`{"genre": ["Comedy","Short"]}`:         true,
		`{"genre": ["Short"]}`:                  false,
		`{"genre": ["Short","Comedy"]}`:         true,
		`{"genre": ["Comedy"]}`:                 false,
	})
}

func TestFindNested(t *testing.T) {
	QueryTest(t, `{
		"selector": {
			"info": {
			   "country": {
				  "$eq": "PL"
			   }
			}
		}
	}`, map[string]bool{
		`{ "info": { "country" : "PL" } }`: true,
		`{ "info": { "country" : "DE" } }`: false,
	})
}

func TestFieldSelector_Match(t *testing.T) {
	tests := []*TestCase{
		// $le
		NewTestCase("Lt int true", SelectorOpLt, 1, 2, true),
		NewTestCase("Lt int false", SelectorOpLt, 2, 2, false),
		NewTestCase("Lt float true", SelectorOpLt, 1.0, 2.0, true),
		NewTestCase("Lt float false", SelectorOpLt, 2.0, 2.0, false),
		NewTestCase("Lt int/float true", SelectorOpLt, 1, 2.0, true),
		NewTestCase("Lt int/float false", SelectorOpLt, 2, 2.0, false),
		NewTestCase("Lt float/int true", SelectorOpLt, 1.0, 2, true),
		NewTestCase("Lt float/int false", SelectorOpLt, 2.0, 2, false),

		// $lte
		NewTestCase("Lte int true", SelectorOpLte, 1, 2, true),
		NewTestCase("Lte int true", SelectorOpLte, 2, 2, true),
		NewTestCase("Lte int false", SelectorOpLte, 3, 2, false),

		// $eq
		NewTestCase("Eq bool true", SelectorOpEq, nil, nil, true),
		NewTestCase("Eq bool false", SelectorOpEq, nil, 123, false),
		NewTestCase("Eq int true", SelectorOpEq, 123, 123, true),
		NewTestCase("Eq int false", SelectorOpEq, 124, 123, false),
		NewTestCase("Eq int(float64) true", SelectorOpEq, float64(123), float64(123), true),
		NewTestCase("Eq int(float64) false", SelectorOpEq, float64(124), float64(123), false),
		NewTestCase("Eq int(float64) true", SelectorOpEq, 123, float64(123), true),
		NewTestCase("Eq string true", SelectorOpEq, "123", "123", true),
		NewTestCase("Eq string false", SelectorOpEq, "124123", "123123", false),
		NewTestCase("Eq int/float true", SelectorOpEq, 2, 2.0, true),
		NewTestCase("Eq int/float false", SelectorOpEq, 1, 2.0, false),
		NewTestCase("Eq float/int true", SelectorOpEq, 1.0, 1, true),
		NewTestCase("Eq float/int false", SelectorOpEq, 2.0, 1, false),

		// $ne
		NewTestCase("Ne int false", SelectorOpNe, 123, 123, false),
		NewTestCase("Ne int true", SelectorOpNe, 124, 123, true),

		// $gte
		NewTestCase("Gt int true", SelectorOpGte, 3, 2, true),
		NewTestCase("Gt int true", SelectorOpGte, 2, 2, true),
		NewTestCase("Gt int false", SelectorOpGte, 1, 2, false),

		// $gt
		NewTestCase("Gt int true", SelectorOpGt, 3, 2, true),
		NewTestCase("Gt int false", SelectorOpGt, 2, 2, false),
		NewTestCase("Gt int false", SelectorOpGt, 1, 2, false),
		NewTestCase("Gt float true", SelectorOpGt, 3.0, 2.0, true),
		NewTestCase("Gt float false", SelectorOpGt, 2.0, 2.0, false),
		NewTestCase("Gt int/float true", SelectorOpGt, 3, 2.0, true),
		NewTestCase("Gt int/float false", SelectorOpGt, 1, 2.0, false),
		NewTestCase("Gt float/int true", SelectorOpGt, 3.0, 2, true),
		NewTestCase("Gt float/int false", SelectorOpGt, 1.0, 2, false),

		// $exists
		NewTestCase("Exists true", SelectorOpExists, 1, 1, true),
		NewTestCase("Exists false", SelectorOpExists, "not exists", 1, false),

		// $type
		NewTestCase("type bool true", SelectorOpType, true, "boolean", true),
		NewTestCase("type number(float) true", SelectorOpType, 3.1, "number", true),
		NewTestCase("type number(int) true", SelectorOpType, 3, "number", true),
		NewTestCase("type string true", SelectorOpType, "asd", "string", true),
		NewTestCase("type array true", SelectorOpType, []int{}, "array", true),
		NewTestCase("type object true", SelectorOpType, struct{}{}, "object", true),
		NewTestCase("type object true", SelectorOpType, map[string]int{}, "object", true),
		NewTestCase("type nil true", SelectorOpType, nil, "null", true),

		// $in
		NewTestCase("In 1/0 false", SelectorOpIn, 1, []interface{}{}, false),
		NewTestCase("In 1/1 true", SelectorOpIn, 1, []interface{}{1}, true),
		NewTestCase("In 1/2 true", SelectorOpIn, 1, []interface{}{2, 1}, true),
		NewTestCase("In -/- false", SelectorOpIn, nil, []interface{}{}, false),
		NewTestCase("In s/is true", SelectorOpIn, "Test", []interface{}{2, "Test"}, true),

		// $nin
		NewTestCase("Nin 1/0 true", SelectorOpNin, 1, []interface{}{}, true),
		NewTestCase("Nin 1/1 false", SelectorOpNin, 1, []interface{}{1}, false),
		NewTestCase("Nin 1/2 false", SelectorOpNin, 1, []interface{}{2, 1}, false),
		NewTestCase("Nin -/- true", SelectorOpNin, nil, []interface{}{}, true),
		NewTestCase("Nin s/is false", SelectorOpNin, "Test", []interface{}{2, "Test"}, false),

		// $size
		NewTestCase("Size 0 true", SelectorOpSize, 0, nil, false),
		NewTestCase("Size 0 true", SelectorOpSize, []interface{}{}, 0, true),
		NewTestCase("Size 1 false", SelectorOpSize, []interface{}{1}, 0, false),
		NewTestCase("Size 1 true", SelectorOpSize, []interface{}{1}, 1, true),
		NewTestCase("Size 2 true", SelectorOpSize, []interface{}{1, 2}, 2, true),
		NewTestCase("Size 2 true", SelectorOpSize, []interface{}{1, 2, 3}, 2, false),

		// $mod
		NewTestCase("Mod 1/-/- false", SelectorOpMod, 1, []interface{}{}, false),
		NewTestCase("Mod 1/1/- false", SelectorOpMod, 1, []interface{}{1}, false),
		NewTestCase("Mod 1/s false", SelectorOpMod, 1, "asd", false),
		NewTestCase("Mod 1/i false", SelectorOpMod, 1, 1, false),
		NewTestCase("Mod 0/1/1 false", SelectorOpMod, 0, []interface{}{1, 1}, false),
		NewTestCase("Mod nil/1/1 false", SelectorOpMod, nil, []interface{}{1, 1}, false),
		NewTestCase("Mod 1/1/0 false", SelectorOpMod, 1, []interface{}{1, 0}, true),
		NewTestCase("Mod 1.0/1/0 false", SelectorOpMod, 1.0, []interface{}{1, 0}, false),
		NewTestCase("Mod 1/1.0/0 false", SelectorOpMod, 1, []interface{}{1.0, 0}, false),
		NewTestCase("Mod 235/7/4 false", SelectorOpMod, 235, []interface{}{7, 4}, true),

		// $regex
		NewTestCase("Regex i/s false", SelectorOpRegex, 0, "0", false),
		NewTestCase("Regex s/i false", SelectorOpRegex, "0", 0, false),
		NewTestCase("Regex s/s true", SelectorOpRegex, "0", "0", true),
		NewTestCase("Regex pattern true", SelectorOpRegex, "0000", "0{4}", true),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := FieldSelector{
				Field:     tt.field,
				Value:     tt.value,
				Operation: tt.operation,
			}
			ok, err := fs.Match(tt.df)
			assert.Equal(t, tt.want, ok,
				fmt.Sprintf("Document %v didn't match expectations: %v", tt.df, err))
		})
	}
}

func QueryTest(t *testing.T, query string, testCases map[string]bool) {
	var fq FindQuery
	err := json.Unmarshal([]byte(query), &fq)
	require.NoError(t, err)
	t.Log("Selector:", fq.Selector.String())

	for raw, result := range testCases {
		doc := Document{}
		err = json.Unmarshal([]byte(raw), &doc.Data)
		assert.NoError(t, err)
		ok, err := fq.Match(&doc)
		assert.Equal(t, result, ok,
			fmt.Sprintf("Document %v didn't match expectations: %v", doc.Data, err))
	}
}

type NoDocumentField struct {
	Value interface{}
}

func (f NoDocumentField) Field(path string) interface{} {
	return f.Value
}

func (f NoDocumentField) Exists(path string) bool {
	return f.Value != "not exists"
}

type TestCase struct {
	name      string
	field     string
	value     interface{}
	operation SelectorOp
	df        DocumentField
	want      bool
}

func NewTestCase(
	name string,
	operation SelectorOp,
	fieldValue, value interface{},
	want bool) *TestCase {
	return &TestCase{
		name:      name,
		value:     value,
		df:        &NoDocumentField{Value: fieldValue},
		operation: operation,
		want:      want,
	}
}
