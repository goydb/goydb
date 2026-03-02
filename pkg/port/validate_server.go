package port

import "context"

// ValidateEngines maps language names to validate builders
type ValidateEngines map[string]ValidateServerBuilder

// ValidateServerBuilder compiles a validate_doc_update function into a ValidateServer
type ValidateServerBuilder func(fn string) (ValidateServer, error)

// ValidateServer executes a compiled validate_doc_update function
type ValidateServer interface {
	// ExecuteValidation runs the validate_doc_update function.
	// It returns nil if validation passes.
	// It returns ErrForbidden or ErrUnauthorized if the function throws.
	ExecuteValidation(ctx context.Context,
		newDoc, oldDoc map[string]interface{},
		userCtx, secObj map[string]interface{}) error
}
