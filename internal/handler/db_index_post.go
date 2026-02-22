package handler

import (
	"encoding/json"
	"net/http"

	"github.com/goydb/goydb/internal/controller"
)

// DBIndexPost handles POST /{db}/_index — create a Mango index.
type DBIndexPost struct {
	Base
}

func (s *DBIndexPost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	var req controller.MangoIndexCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate index type (only "json" supported).
	if req.Type != "" && req.Type != "json" {
		WriteError(w, http.StatusBadRequest, "only index type \"json\" is supported")
		return
	}

	if len(req.Index.Fields) == 0 {
		WriteError(w, http.StatusBadRequest, "index must specify at least one field")
		return
	}

	result, created, err := controller.MangoIndex{DB: db}.Create(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := "exists"
	code := http.StatusOK
	if created {
		status = "created"
		code = http.StatusCreated
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"result": status,
		"id":     result.ID,
		"name":   result.Name,
	})
}
