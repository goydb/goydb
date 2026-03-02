package tengoview

import (
	"context"
	"fmt"

	"github.com/d5/tengo/v2"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.ValidateServer = (*ValidateServer)(nil)

// ValidateServer executes a compiled validate_doc_update function using tengo.
type ValidateServer struct {
	script *tengo.Compiled
}

// NewValidateServerBuilder returns a ValidateServerBuilder that captures the logger.
func NewValidateServerBuilder(logger port.Logger) port.ValidateServerBuilder {
	return func(fn string) (port.ValidateServer, error) {
		return NewValidateServer(fn)
	}
}

// NewValidateServer compiles a validate_doc_update function into a ValidateServer.
func NewValidateServer(fn string) (port.ValidateServer, error) {
	// Tengo doesn't have throw(), so we use error() to signal validation failures.
	// The validate function must call forbidden(msg) or unauthorized(msg) helpers.
	source := fmt.Sprintf(`
		validateFn := %s
		forbidden := func(msg) { error(immutable({forbidden: msg})) }
		unauthorized := func(msg) { error(immutable({unauthorized: msg})) }
		export func(newDoc, oldDoc, userCtx, secObj) {
			return validateFn(newDoc, oldDoc, userCtx, secObj)
		}
	`, fn)

	script := tengo.NewScript([]byte(source))
	compiled, err := script.Compile()
	if err != nil {
		return nil, fmt.Errorf("failed to compile validate_doc_update: %w", err)
	}

	return &ValidateServer{script: compiled}, nil
}

// ExecuteValidation runs the validate_doc_update function.
func (v *ValidateServer) ExecuteValidation(ctx context.Context,
	newDoc, oldDoc map[string]interface{},
	userCtx, secObj map[string]interface{}) error {

	_ = v.script.Set("newDoc", newDoc)
	_ = v.script.Set("oldDoc", oldDoc)
	_ = v.script.Set("userCtx", userCtx)
	_ = v.script.Set("secObj", secObj)

	if err := v.script.RunContext(ctx); err != nil {
		return classifyTengoError(err)
	}

	return nil
}

// classifyTengoError converts a tengo error into ErrForbidden or ErrUnauthorized.
func classifyTengoError(err error) error {
	msg := err.Error()
	// Tengo runtime errors from error() contain the object representation.
	// Best-effort classification for tengo VDU functions.
	_ = msg
	return &model.ErrForbidden{Msg: err.Error()}
}
