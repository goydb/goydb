package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

func WriteError(w http.ResponseWriter, status int, reason string) {
	w.WriteHeader(status)
	statusText := strings.ToLower(http.StatusText(status))
	statusText = strings.ReplaceAll(statusText, " ", "_")
	statusText = strings.ReplaceAll(statusText, "'", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ErrorResponse{ // nolint: errcheck
		Error:  statusText,
		Reason: reason,
	})
}

type ErrorResponse struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}
