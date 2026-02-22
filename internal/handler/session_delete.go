package handler

import (
	"net/http"
)

type SessionDelete struct {
	Base
}

func (s *SessionDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	http.SetCookie(w, &http.Cookie{Name: sessionName, MaxAge: -1})
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`)) // nolint: errcheck
}
