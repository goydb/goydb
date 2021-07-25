package handler

import (
	"encoding/json"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

const sessionName = "AuthSession"

type SessionGet struct {
	Base
}

func (s *SessionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	session, via := Authenticator{Base: s.Base}.Auth(r)
	if session == nil {
		WriteError(w, http.StatusBadRequest, "session is invalid")
		return
	}

	resp := &SessionResponse{
		Ok: true,
		SessionInfo: SessionInfo{
			AuthenticationHandlers: []string{"cookie", "default"},
		},
	}

	if session.Authenticated() {
		resp.SessionUserCtx = *session
		resp.SessionInfo.Authenticated = via
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // nolint: errcheck
}

type SessionResponse struct {
	Ok             bool          `json:"ok"`
	SessionUserCtx model.Session `json:"userCtx"`
	SessionInfo    SessionInfo   `json:"info"`
}

type SessionInfo struct {
	AuthenticationHandlers []string `json:"authentication_handlers"`
	Authenticated          string   `json:"authenticated,omitempty"`
}
