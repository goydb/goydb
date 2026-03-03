package handler

import (
	"encoding/json"
	"net/http"
)

const sessionName = "AuthSession"

type SessionGet struct {
	Base
}

func (s *SessionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	session, via := Authenticator{Base: s.Base}.Auth(r)

	chain := buildAuthChain(s.Base)

	resp := &SessionResponse{
		Ok: true,
		SessionInfo: SessionInfo{
			AuthenticationHandlers: authHandlerNames(chain),
			AuthenticationDB:       "_users",
		},
		SessionUserCtx: SessionUserCtx{
			Roles: []string{},
		},
	}

	if session != nil && session.Authenticated() {
		name := session.Name
		resp.SessionUserCtx.Name = &name
		resp.SessionUserCtx.Roles = session.Roles
		resp.SessionInfo.Authenticated = via
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // nolint: errcheck
}

type SessionUserCtx struct {
	Name  *string  `json:"name"`
	Roles []string `json:"roles"`
}

type SessionResponse struct {
	Ok             bool           `json:"ok"`
	SessionUserCtx SessionUserCtx `json:"userCtx"`
	SessionInfo    SessionInfo    `json:"info"`
}

type SessionInfo struct {
	AuthenticationHandlers []string `json:"authentication_handlers"`
	AuthenticationDB       string   `json:"authentication_db"`
	Authenticated          string   `json:"authenticated,omitempty"`
}
