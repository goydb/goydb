package tengoview

import (
	"context"
	"fmt"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.ViewServer = (*ViewServer)(nil)

type ViewServer struct {
	script   *tengo.Script
	compiled *tengo.Compiled
}

func NewViewServer(fn string) (port.ViewServer, error) {
	fn = `text := import("text")
	math := import("math")
	times := import("times")
	rand := import("rand")
	fmt := import("fmt")
	json := import("json")
	enum := import("enum")
	hex := import("hex")
	base64 := import("base64")

	_result := []
	_doc := {}
	_index := []
	emit := func (key, value) {
		_result = _result + [[ key, value, _doc._id ]]
	}
	index := func (name, value, opts) {
		_index = _index + [[ name, value, opts ]]
	}
	docFn := ` + fn + `
	for doc in docs {
		_doc = doc
		_index = []
		docFn(doc)
		if len(_index) > 0 {
			_result = _result + [[_doc._id, _index]]
		}
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
		return nil, fmt.Errorf("script error %v: %w", fn, err)
	}

	return &ViewServer{
		script:   script,
		compiled: compiled,
	}, nil
}

func (s *ViewServer) ExecuteView(ctx context.Context, docs []*model.Document) ([]*model.Document, error) {
	err := s.setDocs(docs)
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

func (s *ViewServer) ExecuteSearch(ctx context.Context, docs []*model.Document) ([]*model.Document, error) {
	err := s.setDocs(docs)
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
		// fmt.Println(row)
		sid := &model.Document{
			ID:      row[0].(string),
			Fields:  make(map[string]interface{}),
			Options: make(map[string]model.SearchIndexOption),
		}

		indexRecords := row[1].([]interface{})
		for _, ir := range indexRecords {
			v := ir.([]interface{})
			field := v[0].(string)
			value := v[1]
			options := v[2].(map[string]interface{})

			sid.Fields[field] = value
			sio := model.SearchIndexOption{
				// TODO Boost: options["boost"],
				Store: options["store"] == true,
				Facet: options["facet"] == true,
			}
			if index, ok := options["facet"]; ok {
				v := (index == true)
				sio.Index = &v
			}
			sid.Options[field] = sio
		}

		result[i] = sid
	}

	return result, nil
}

func (s *ViewServer) setDocs(docs []*model.Document) error {
	simpleDocs := make([]interface{}, len(docs))
	for i, doc := range docs {
		doc.Data["_id"] = doc.ID
		doc.Data["_rev"] = doc.Rev
		simpleDocs[i] = doc.Data
	}

	return s.compiled.Set("docs", simpleDocs)
}
