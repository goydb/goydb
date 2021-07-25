package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/goydb/goydb/pkg/model"
)

type SessionPost struct {
	Base
}

func (s *SessionPost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	err := r.ParseForm()
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	username := strings.Join(r.Form["name"], "")
	password := strings.Join(r.Form["password"], "")

	sb := Authenticator{Base: s.Base}.Authenticate(r.Context(), username, password)

	if sb == nil {
		WriteError(w, http.StatusUnauthorized, "Name or password is incorrect.")
		return
	}

	session, err := s.SessionStore.New(r, sessionName)
	adminSession := sb.Session()
	adminSession.Store(session.Values)

	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := &SessionPostResponse{
		Ok:      true,
		Session: adminSession,
	}

	w.Header().Set("Content-Type", "application/json")
	err = session.Save(r, w)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to save.")
		return
	}
	json.NewEncoder(w).Encode(resp) // nolint: errcheck
}

type SessionPostResponse struct {
	Ok             bool `json:"ok"`
	*model.Session `json:"userCtx,omitempty"`
}
