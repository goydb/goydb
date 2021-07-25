package handler

import (
	"encoding/json"
	"net/http"
)

type DBEnsureFullCommit struct {
	Base
}

func (s *DBEnsureFullCommit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	resp := EnsureFullCommitResponse{
		Ok:                true,
		InstanceStartTime: "0",
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // nolint: errcheck
}

type EnsureFullCommitResponse struct {
	Ok                bool   `json:"ok"`
	InstanceStartTime string `json:"instance_start_time"`
}
