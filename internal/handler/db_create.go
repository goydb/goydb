package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type DBCreate struct {
	Base
}

func (s *DBCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	dbName := mux.Vars(r)["db"]
	db, err := s.Storage.Database(r.Context(), dbName)
	if db != nil {
		WriteError(w, http.StatusConflict, "Database already exists.")
		return
	}

	_, err = s.Storage.CreateDatabase(r.Context(), dbName)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := DBResponse{
		DbName:            mux.Vars(r)["db"],
		PurgeSeq:          "0",
		UpdateSeq:         "0",
		DiskFormatVersion: 8,
		InstanceStartTime: "0",
		DocCount:          0,
		DocDelCount:       0,
		CompactRunning:    false,
		Sizes: Sizes{
			File:     20840,
			External: 5578,
			Active:   2736,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response) // nolint: errcheck
}
