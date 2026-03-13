package handler

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
)

// pathVar returns the URL-decoded value of a named path variable.
// When the router uses UseEncodedPath(), mux.Vars returns raw
// (percent-encoded) values; this helper decodes them so that
// document IDs containing slashes (encoded as %2F) are handled
// correctly.
func pathVar(r *http.Request, name string) string {
	v := mux.Vars(r)[name]
	if decoded, err := url.PathUnescape(v); err == nil {
		return decoded
	}
	return v
}

// pathVarOk returns the URL-decoded value and whether it existed.
func pathVarOk(r *http.Request, name string) (string, bool) {
	v, ok := mux.Vars(r)[name]
	if !ok {
		return "", false
	}
	if decoded, err := url.PathUnescape(v); err == nil {
		return decoded, true
	}
	return v, true
}
