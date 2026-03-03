package handler

import (
	"net/http"
	"strings"

	"github.com/goydb/goydb/pkg/model"
)

// AuthHandler is a pluggable authentication handler. Implementations attempt
// to authenticate the request and return a session on success.
type AuthHandler interface {
	// Name returns the short name used in the /_session response
	// (e.g. "cookie", "default", "proxy", "jwt").
	Name() string

	// Authenticate inspects the request and returns a session if the request
	// can be authenticated by this handler. The bool indicates whether
	// authentication succeeded.
	Authenticate(r *http.Request) (*model.Session, bool)
}

// authHandlerBuilder is a factory that creates an AuthHandler from a Base.
type authHandlerBuilder func(b Base) AuthHandler

// authHandlerRegistry maps config names to builder functions.
var authHandlerRegistry = map[string]authHandlerBuilder{
	"cookie_authentication_handler":  func(b Base) AuthHandler { return &CookieAuthHandler{Base: b} },
	"default_authentication_handler": func(b Base) AuthHandler { return &DefaultAuthHandler{Base: b} },
}

// RegisterAuthHandler adds a named handler builder to the global registry.
func RegisterAuthHandler(name string, builder authHandlerBuilder) {
	authHandlerRegistry[name] = builder
}

// buildAuthChain reads the httpd/authentication_handlers config key and
// returns the ordered list of AuthHandlers.
func buildAuthChain(b Base) []AuthHandler {
	raw, _ := b.Config.Get("httpd", "authentication_handlers")
	parts := strings.Split(raw, ",")

	var chain []AuthHandler
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		if builder, ok := authHandlerRegistry[name]; ok {
			chain = append(chain, builder(b))
		}
	}

	// Fallback: if nothing was configured, use cookie + default.
	if len(chain) == 0 {
		chain = []AuthHandler{
			&CookieAuthHandler{Base: b},
			&DefaultAuthHandler{Base: b},
		}
	}

	return chain
}

// authHandlerNames returns the short names of all handlers in the chain.
func authHandlerNames(chain []AuthHandler) []string {
	names := make([]string, len(chain))
	for i, h := range chain {
		names[i] = h.Name()
	}
	return names
}

// CookieAuthHandler restores a session from the gorilla session cookie.
type CookieAuthHandler struct {
	Base Base
}

func (h *CookieAuthHandler) Name() string { return "cookie" }

func (h *CookieAuthHandler) Authenticate(r *http.Request) (*model.Session, bool) {
	session, err := h.Base.SessionStore.Get(r, sessionName)
	if err != nil {
		return nil, false
	}
	if session.IsNew {
		return nil, false
	}
	var s model.Session
	s.Restore(session.Values)
	if !s.Authenticated() {
		return nil, false
	}
	return &s, true
}

// DefaultAuthHandler performs HTTP Basic Authentication.
type DefaultAuthHandler struct {
	Base Base
}

func (h *DefaultAuthHandler) Name() string { return "default" }

func (h *DefaultAuthHandler) Authenticate(r *http.Request) (*model.Session, bool) {
	username, password, ok := r.BasicAuth()
	if !ok {
		return nil, false
	}
	user := Authenticator{Base: h.Base}.Authenticate(r.Context(), username, password)
	if user == nil {
		return nil, false
	}
	return user.Session(), true
}
