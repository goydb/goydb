package gojaview

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.ValidateServer = (*ValidateServer)(nil)

// ValidateServer executes a compiled validate_doc_update function using goja.
type ValidateServer struct {
	vm     *goja.Runtime
	logger port.Logger
}

// NewValidateServerBuilder returns a ValidateServerBuilder that captures the logger.
func NewValidateServerBuilder(logger port.Logger) port.ValidateServerBuilder {
	return func(fn string) (port.ValidateServer, error) {
		return NewValidateServer(fn, logger)
	}
}

// NewValidateServer compiles a validate_doc_update function into a ValidateServer.
func NewValidateServer(fn string, logger port.Logger) (port.ValidateServer, error) {
	vm := goja.New()

	// Inject log() so VDU functions can write to the server log.
	_ = vm.Set("log", func(msg interface{}) {
		logger.Infof(context.Background(), "vdu log", "message", msg)
	})

	// Wrap the user function with isArray and an executeValidation wrapper.
	script := fmt.Sprintf(`
		var isArray = Array.isArray;
		var validateFn = %s;
		function executeValidation(newDoc, oldDoc, userCtx, secObj) {
			validateFn(newDoc, oldDoc, userCtx, secObj);
		}
	`, fn)

	_, err := vm.RunString(script)
	if err != nil {
		return nil, fmt.Errorf("failed to compile validate_doc_update function: %w", err)
	}

	return &ValidateServer{vm: vm, logger: logger}, nil
}

// ExecuteValidation runs the validate_doc_update function.
func (v *ValidateServer) ExecuteValidation(ctx context.Context,
	newDoc, oldDoc map[string]interface{},
	userCtx, secObj map[string]interface{}) error {

	var executeValidation goja.Callable
	if err := v.vm.ExportTo(v.vm.Get("executeValidation"), &executeValidation); err != nil {
		return err
	}

	// Convert oldDoc: pass null to JS if nil.
	var oldDocVal goja.Value
	if oldDoc == nil {
		oldDocVal = goja.Null()
	} else {
		oldDocVal = v.vm.ToValue(oldDoc)
	}

	_, err := executeValidation(goja.Undefined(),
		v.vm.ToValue(newDoc),
		oldDocVal,
		v.vm.ToValue(userCtx),
		v.vm.ToValue(secObj),
	)
	if err != nil {
		return classifyJSError(err)
	}

	return nil
}

// classifyJSError converts a goja exception into ErrForbidden or ErrUnauthorized.
func classifyJSError(err error) error {
	ex, ok := err.(*goja.Exception)
	if !ok {
		return err
	}

	val := ex.Value().Export()
	m, ok := val.(map[string]interface{})
	if !ok {
		return err
	}

	if msg, ok := m["forbidden"]; ok {
		return &model.ErrForbidden{Msg: fmt.Sprint(msg)}
	}
	if msg, ok := m["unauthorized"]; ok {
		return &model.ErrUnauthorized{Msg: fmt.Sprint(msg)}
	}

	return err
}
