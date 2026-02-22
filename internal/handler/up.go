package handler

import (
	"encoding/json"
	"net/http"
)

type Up struct {
	Base
}

func (s *Up) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) // nolint: errcheck
}
