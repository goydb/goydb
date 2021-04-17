package tengoview

import (
	"context"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
	"github.com/goydb/goydb/pkg/model"
)

type ViewServer struct {
	script   *tengo.Script
	compiled *tengo.Compiled
}

func NewViewServer(fn string) (*ViewServer, error) {
	fn = `math := import("math")
	fmt := import("fmt")
	_result := []
	_doc := {}
	emit := func (key, value) {
		_result = _result + [[key, value, _doc._id]]
	}
	docFn := ` + fn + `
	for doc in docs {
		_doc = doc
		docFn(doc)
	}`
	script := tengo.NewScript([]byte(fn))
	script.SetImports(stdlib.GetModuleMap(
		"text",   // regular expressions, string conversion, and manipulation
		"math",   // mathematical constants and functions
		"times",  // time-related functions
		"rand",   // random functions
		"fmt",    // formatting functions
		"json",   // JSON functions
		"enum",   // Enumeration functions
		"hex",    // hex encoding and decoding functions
		"base64", // base64 encoding and decoding functions
	))

	script.Add("docs", []interface{}{})

	compiled, err := script.Compile()
	if err != nil {
		return nil, err
	}

	return &ViewServer{
		script:   script,
		compiled: compiled,
	}, nil
}

func (s *ViewServer) Process(ctx context.Context, docs []*model.Document) ([]*model.Document, error) {
	simpleDocs := make([]interface{}, len(docs))
	for i, doc := range docs {
		doc.Data["_id"] = doc.ID
		doc.Data["_rev"] = doc.Rev
		simpleDocs[i] = doc.Data
	}

	err := s.compiled.Set("docs", simpleDocs)
	if err != nil {
		return nil, err
	}

	err = s.compiled.RunContext(ctx)
	if err != nil {
		return nil, err
	}

	resultData := s.compiled.Get("_result").Array()
	result := make([]*model.Document, len(resultData))

	for i, rd := range resultData {
		row := rd.([]interface{})
		// fmt.Println(i, row)
		result[i] = &model.Document{
			Key:   row[0],
			Value: row[1],
			ID:    row[2].(string),
		}
	}

	return result, nil
}
