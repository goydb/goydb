package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/goydb/goydb/pkg/model"
)

type ActiveTasks struct {
	Base
}

func (s *ActiveTasks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	names, err := s.Storage.Databases(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tasks := make([]*Task, 0, 10)
	for _, name := range names {
		db, err := s.Storage.Database(r.Context(), name)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		dbtasks, err := db.PeekTasks(r.Context(), 100)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		for _, task := range dbtasks {
			// Map action to CouchDB-compatible type
			taskType := "indexer"
			switch task.Action {
			case model.ActionUpdateSearch:
				taskType = "search_indexer"
			case model.ActionUpdateMango:
				taskType = "indexer"
			}

			// Extract design document ID from DesignDocFn (format: "type:docname:fnname")
			designDoc := task.DesignDocFn
			if parts := strings.SplitN(designDoc, ":", 3); len(parts) == 3 {
				designDoc = "_design/" + parts[1]
			}

			// Use UpdatedAt for updated_on if available, fall back to ActiveSince
			updatedOn := task.ActiveSince
			if !task.UpdatedAt.IsZero() {
				updatedOn = task.UpdatedAt
			}

			tasks = append(tasks, &Task{
				Node:           "nonode@nohost",
				Pid:            fmt.Sprintf("<%d.%d>", os.Getpid(), task.ID),
				ChangesDone:    task.Processed,
				TotalChanges:   task.ProcessingTotal,
				Database:       name,
				DesignDocument: designDoc,
				Progress:       int(float64(task.Processed) / float64(task.ProcessingTotal) * 100.0),
				StartedOn:      int(task.ActiveSince.Unix()),
				Type:           taskType,
				UpdatedOn:      int(updatedOn.Unix()),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks) // nolint: errcheck
}

type Task struct {
	Node           string `json:"node"`
	Pid            string `json:"pid"`
	ChangesDone    int    `json:"changes_done"`
	Database       string `json:"database"`
	DesignDocument string `json:"design_document"`
	Progress       int    `json:"progress"`
	StartedOn      int    `json:"started_on"` // unix time
	TotalChanges   int    `json:"total_changes"`
	Type           string `json:"type"`
	UpdatedOn      int    `json:"updated_on"` // unix time
}
