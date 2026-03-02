package port

import "context"

// UpdateEngines maps language names to update builders
type UpdateEngines map[string]UpdateServerBuilder

// UpdateServerBuilder compiles an update function into an UpdateServer
type UpdateServerBuilder func(fn string) (UpdateServer, error)

// UpdateServer executes a compiled update function
type UpdateServer interface {
	// ExecuteUpdate runs the update function with the given document and request.
	// doc may be nil if no docid was provided or the document doesn't exist.
	// Returns the result containing the document to save and the response.
	ExecuteUpdate(ctx context.Context, doc map[string]interface{}, req map[string]interface{}) (*UpdateResult, error)
}

// UpdateResult holds the result of an update function execution.
type UpdateResult struct {
	// Doc is the document to save, or nil if the function chose not to save.
	Doc map[string]interface{}
	// Code is the HTTP status code (from response object), 0 if not set.
	Code int
	// Headers are custom HTTP headers from the response object.
	Headers map[string]string
	// Body is the response body as a string (mutually exclusive with JSON).
	Body string
	// JSON is the response body as a JSON-serializable value.
	JSON interface{}
}
