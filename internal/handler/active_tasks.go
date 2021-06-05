package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
)

type ActiveTasks struct {
	Base
}

func (s *ActiveTasks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

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
			tasks = append(tasks, &Task{
				Node:           "nonode@nohost",
				Pid:            fmt.Sprintf("<%d.%d>", os.Getpid(), task.ID),
				ChangesDone:    task.Processed,
				TotalChanges:   task.ProcessingTotal,
				Database:       name,
				DesignDocument: task.Ddfn,
				Phase:          strconv.Itoa(int(task.Action)),
				Progress:       int(float64(task.Processed) / float64(task.ProcessingTotal) * 100.0),
				StartedOn:      int(task.ActiveSince.Unix()),
				Type:           "indexer",
				UpdatedOn:      int(task.ActiveSince.Unix()),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

type Task struct {
	Node           string `json:"node"`
	Pid            string `json:"pid"`
	ChangesDone    int    `json:"changes_done"`
	Database       string `json:"database"`
	DesignDocument string `json:"design_document"`
	Phase          string `json:"phase"`
	Progress       int    `json:"progress"`
	StartedOn      int    `json:"started_on"` // unix time
	TotalChanges   int    `json:"total_changes"`
	Type           string `json:"type"`
	UpdatedOn      int    `json:"updated_on"` // unix time
}
