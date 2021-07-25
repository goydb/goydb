package handler

import (
	"net/http"
)

type SessionDelete struct {
	Base
}

func (s *SessionDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	session, err := s.SessionStore.New(r, sessionName)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if session.IsNew {
		WriteError(w, http.StatusBadRequest, "can't logout if not logged in")
		return
	}

	c := http.Cookie{Name: sessionName, MaxAge: -1}
	http.SetCookie(w, &c)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`)) // nolint: errcheck
}
