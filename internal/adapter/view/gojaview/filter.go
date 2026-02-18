package gojaview

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.FilterServer = (*FilterServer)(nil)

type FilterServer struct {
	vm *goja.Runtime
}

// NewFilterServer compiles a filter function into a FilterServer
func NewFilterServer(fn string) (port.FilterServer, error) {
	vm := goja.New()

	// Wrap filter function
	script := fmt.Sprintf(`
		var filterFn = %s;
		function executeFilter(doc, req) {
			return filterFn(doc, req);
		}
	`, fn)

	_, err := vm.RunString(script)
	if err != nil {
		return nil, fmt.Errorf("failed to compile filter function: %w", err)
	}

	return &FilterServer{vm: vm}, nil
}

// ExecuteFilter tests if a document passes the filter
func (f *FilterServer) ExecuteFilter(ctx context.Context, doc *model.Document, req map[string]interface{}) (bool, error) {
	// Convert document to plain JS object
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

	// Call filter function
	var executeFilter goja.Callable
	if err := f.vm.ExportTo(f.vm.Get("executeFilter"), &executeFilter); err != nil {
		return false, err
	}

	result, err := executeFilter(goja.Undefined(), f.vm.ToValue(simpleDoc), f.vm.ToValue(req))
	if err != nil {
		return false, err
	}

	// Convert result to boolean
	return result.ToBoolean(), nil
}
