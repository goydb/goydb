package gojaview

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.UpdateServer = (*UpdateServer)(nil)

// UpdateServer executes a compiled update function using goja.
type UpdateServer struct {
	vm *goja.Runtime
}

// NewUpdateServer compiles an update function into an UpdateServer.
func NewUpdateServer(fn string) (port.UpdateServer, error) {
	vm := goja.New()

	script := fmt.Sprintf(`
		var isArray = Array.isArray;
		var updateFn = %s;
		function executeUpdate(doc, req) {
			return updateFn(doc, req);
		}
	`, fn)

	_, err := vm.RunString(script)
	if err != nil {
		return nil, fmt.Errorf("failed to compile update function: %w", err)
	}

	return &UpdateServer{vm: vm}, nil
}

// ExecuteUpdate runs the update function and parses the [doc, response] result.
func (u *UpdateServer) ExecuteUpdate(ctx context.Context, doc map[string]interface{}, req map[string]interface{}) (*port.UpdateResult, error) {
	var executeUpdate goja.Callable
	if err := u.vm.ExportTo(u.vm.Get("executeUpdate"), &executeUpdate); err != nil {
		return nil, err
	}

	var docVal goja.Value
	if doc == nil {
		docVal = goja.Null()
	} else {
		docVal = u.vm.ToValue(doc)
	}

	result, err := executeUpdate(goja.Undefined(), docVal, u.vm.ToValue(req))
	if err != nil {
		return nil, fmt.Errorf("update function error: %w", err)
	}

	exported := result.Export()
	arr, ok := exported.([]interface{})
	if !ok {
		return nil, fmt.Errorf("update function must return an array, got %T", exported)
	}
	if len(arr) != 2 {
		return nil, fmt.Errorf("update function must return a 2-element array, got %d elements", len(arr))
	}

	return parseUpdateResult(arr)
}

// parseUpdateResult parses the [doc, response] array from an update function.
func parseUpdateResult(arr []interface{}) (*port.UpdateResult, error) {
	res := &port.UpdateResult{}

	// First element: document to save (or null)
	if arr[0] != nil {
		docMap, ok := arr[0].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("first element of update result must be an object or null, got %T", arr[0])
		}
		res.Doc = docMap
	}

	// Second element: response (string or object)
	switch resp := arr[1].(type) {
	case string:
		res.Body = resp
	case map[string]interface{}:
		if code, ok := resp["code"]; ok {
			switch c := code.(type) {
			case int64:
				res.Code = int(c)
			case float64:
				res.Code = int(c)
			}
		}
		if headers, ok := resp["headers"].(map[string]interface{}); ok {
			res.Headers = make(map[string]string)
			for k, v := range headers {
				if s, ok := v.(string); ok {
					res.Headers[k] = s
				}
			}
		}
		if body, ok := resp["body"].(string); ok {
			res.Body = body
		}
		if jsonVal, ok := resp["json"]; ok {
			res.JSON = jsonVal
		}
	case nil:
		// empty response
	default:
		return nil, fmt.Errorf("second element of update result must be a string or object, got %T", arr[1])
	}

	return res, nil
}
