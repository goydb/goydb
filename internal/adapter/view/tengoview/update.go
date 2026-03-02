package tengoview

import (
	"context"
	"fmt"

	"github.com/d5/tengo/v2"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.UpdateServer = (*UpdateServer)(nil)

// UpdateServer executes a compiled update function using tengo.
type UpdateServer struct {
	script *tengo.Compiled
}

// NewUpdateServer compiles an update function into an UpdateServer.
func NewUpdateServer(fn string) (port.UpdateServer, error) {
	source := fmt.Sprintf(`
		updateFn := %s
		_result := updateFn(doc, req)
	`, fn)

	script := tengo.NewScript([]byte(source))
	script.Add("doc", nil)  //nolint:errcheck
	script.Add("req", nil)  //nolint:errcheck

	compiled, err := script.Compile()
	if err != nil {
		return nil, fmt.Errorf("failed to compile update function: %w", err)
	}

	return &UpdateServer{script: compiled}, nil
}

// ExecuteUpdate runs the update function and parses the [doc, response] result.
func (u *UpdateServer) ExecuteUpdate(ctx context.Context, doc map[string]interface{}, req map[string]interface{}) (*port.UpdateResult, error) {
	_ = u.script.Set("doc", doc)
	_ = u.script.Set("req", req)

	if err := u.script.RunContext(ctx); err != nil {
		return nil, fmt.Errorf("update function error: %w", err)
	}

	resultVar := u.script.Get("_result")
	if resultVar == nil {
		return nil, fmt.Errorf("update function did not return a result")
	}

	arr, ok := resultVar.Value().([]interface{})
	if !ok {
		return nil, fmt.Errorf("update function must return an array, got %T", resultVar.Value())
	}
	if len(arr) != 2 {
		return nil, fmt.Errorf("update function must return a 2-element array, got %d elements", len(arr))
	}

	return parseTengoUpdateResult(arr)
}

// parseTengoUpdateResult parses the [doc, response] array from a tengo update function.
func parseTengoUpdateResult(arr []interface{}) (*port.UpdateResult, error) {
	res := &port.UpdateResult{}

	// First element: document to save (or nil)
	if arr[0] != nil {
		docMap, ok := arr[0].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("first element of update result must be a map or nil, got %T", arr[0])
		}
		res.Doc = docMap
	}

	// Second element: response (string or map)
	switch resp := arr[1].(type) {
	case string:
		res.Body = resp
	case map[string]interface{}:
		if code, ok := resp["code"]; ok {
			switch c := code.(type) {
			case int64:
				res.Code = int(c)
			case int:
				res.Code = c
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
		return nil, fmt.Errorf("second element of update result must be a string or map, got %T", arr[1])
	}

	return res, nil
}
