package handler

import (
	"net/http"
)

// DDShowFunction handles GET/POST /{db}/_design/{ddoc}/_show/{func}[/{docid}].
// Show functions are a CouchDB 1.x feature that transforms documents using
// server-side JavaScript. Returns 501 Not Implemented.
type DDShowFunction struct {
	Base
}

func (s *DDShowFunction) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	WriteError(w, http.StatusNotImplemented, "show functions are not supported")
}

// DDListFunction handles GET/POST /{db}/_design/{ddoc}/_list/{func}/{view}.
// List functions are a CouchDB 1.x feature. Returns 501 Not Implemented.
type DDListFunction struct {
	Base
}

func (s *DDListFunction) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	WriteError(w, http.StatusNotImplemented, "list functions are not supported")
}

// DDUpdateFunction handles POST/PUT /{db}/_design/{ddoc}/_update/{func}[/{docid}].
// Update functions are server-side JavaScript handlers that can create or modify
// documents. The implementation is in ddoc_update.go.
type DDUpdateFunction struct {
	Base
}

// DDRewrite handles ALL /{db}/_design/{ddoc}/_rewrite/{path:.*}.
// Rewrite rules are a CouchDB 1.x feature. Returns 501 Not Implemented.
type DDRewrite struct {
	Base
}

func (s *DDRewrite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	WriteError(w, http.StatusNotImplemented, "rewrite rules are not supported")
}
