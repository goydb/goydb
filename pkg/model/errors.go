package model

import "fmt"

// ErrForbidden represents a CouchDB {forbidden: "msg"} error from validate_doc_update.
type ErrForbidden struct {
	Msg string
}

func (e *ErrForbidden) Error() string {
	return fmt.Sprintf("forbidden: %s", e.Msg)
}

// ErrUnauthorized represents a CouchDB {unauthorized: "msg"} error from validate_doc_update.
type ErrUnauthorized struct {
	Msg string
}

func (e *ErrUnauthorized) Error() string {
	return fmt.Sprintf("unauthorized: %s", e.Msg)
}
