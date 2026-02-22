package tengoview

import (
	"context"
	"fmt"

	"github.com/d5/tengo/v2"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.FilterServer = (*FilterServer)(nil)

type FilterServer struct {
	script *tengo.Compiled
}

func NewFilterServer(fn string) (port.FilterServer, error) {
	// Wrap filter function
	source := fmt.Sprintf(`
		filterFn := %s
		export func(doc, req) {
			return filterFn(doc, req)
		}
	`, fn)

	script := tengo.NewScript([]byte(source))
	compiled, err := script.Compile()
	if err != nil {
		return nil, fmt.Errorf("failed to compile filter: %w", err)
	}

	return &FilterServer{script: compiled}, nil
}

func (f *FilterServer) ExecuteFilter(ctx context.Context, doc *model.Document, req map[string]interface{}) (bool, error) {
	// Convert document to tengo object
	simpleDoc := map[string]interface{}{
		"_id":  doc.ID,
		"_rev": doc.Rev,
	}
	if doc.Deleted {
		simpleDoc["_deleted"] = true
	}
	for k, v := range doc.Data {
		if k != "_id" && k != "_rev" && k != "_deleted" {
			simpleDoc[k] = v
		}
	}

	// Set variables
	_ = f.script.Set("doc", simpleDoc)
	_ = f.script.Set("req", req)

	// Run script
	if err := f.script.Run(); err != nil {
		return false, err
	}

	// Get result
	result := f.script.Get("result")
	if result == nil {
		return false, nil
	}

	return result.Bool(), nil
}
