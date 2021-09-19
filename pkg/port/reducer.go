package port

import "github.com/goydb/goydb/pkg/model"

type ReducerEngines map[string]ReducerServerBuilder

type ReducerServerBuilder func(fn string) (Reducer, error)

type Reducer interface {
	Reduce(doc *model.Document, group bool)
	Result() []*model.Document
}
