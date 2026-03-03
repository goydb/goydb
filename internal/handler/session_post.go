package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

type SessionPost struct {
	Base
}

func (s *SessionPost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	var username, password, totpToken string

	ct := r.Header.Get("Content-Type")
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}

	if ct == "application/json" {
		var body struct {
			Name     string `json:"name"`
			Password string `json:"password"`
			Token    string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		username = body.Name
		password = body.Password
		totpToken = body.Token
	} else {
		if err := r.ParseForm(); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		username = strings.Join(r.Form["name"], "")
		password = strings.Join(r.Form["password"], "")
		totpToken = strings.Join(r.Form["token"], "")
	}

	auth := Authenticator{Base: s.Base}
	sb, user := auth.AuthenticateWithUser(r.Context(), username, password)

	if sb == nil {
		WriteError(w, http.StatusUnauthorized, "Name or password is incorrect.")
		return
	}

	// TOTP verification: if the user has TOTP configured, require and verify the token.
	// Admin users (user == nil) skip TOTP — they have no user document.
	if user != nil {
		if err := checkTOTP(user, totpToken); err != nil {
			WriteError(w, http.StatusUnauthorized, err.Error())
			return
		}
	}

	session, err := s.SessionStore.New(r, sessionName)
	adminSession := sb.Session()
	adminSession.Store(session.Values)

	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	roles := adminSession.Roles
	if roles == nil {
		roles = []string{}
	}
	resp := &SessionPostResponse{
		Ok:    true,
		Name:  adminSession.Name,
		Roles: roles,
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
	Ok    bool     `json:"ok"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}
