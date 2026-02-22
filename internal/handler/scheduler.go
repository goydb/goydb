package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type SchedulerJob struct {
	Database  string   `json:"database"`
	ID        string   `json:"id"`
	Pid       string   `json:"pid"`
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	User      *string  `json:"user"`
	DocID     string   `json:"doc_id"`
	History   []string `json:"history"`
	Node      string   `json:"node"`
	StartTime string   `json:"start_time"`
	Status    string   `json:"status"`
}

type SchedulerJobs struct {
	Base
}

func (s *SchedulerJobs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	ctx := r.Context()
	jobs := make([]SchedulerJob, 0)

	db, err := s.Storage.Database(ctx, "_replicator")
	if err != nil {
		// _replicator doesn't exist yet — return empty response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
			"total_rows": 0,
			"offset":     0,
			"jobs":       jobs,
		})
		return
	}

	docs, _, err := db.AllDocs(ctx, port.AllDocsQuery{
		IncludeDocs: true,
		SkipLocal:   true,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for i, doc := range docs {
		if doc.IsDesignDoc() {
			continue
		}

		repDoc, err := model.ParseReplicationDoc(doc)
		if err != nil {
			continue
		}

		status := replicationStateToSchedulerStatus(repDoc.ReplicationState)

		startTime := time.Time{}
		if t, ok := doc.Data["_replication_state_time"].(string); ok && t != "" {
			if parsed, err := time.Parse(time.RFC3339, t); err == nil {
				startTime = parsed
			}
		}
		startTimeStr := startTime.UTC().Format(time.RFC3339)

		jobs = append(jobs, SchedulerJob{
			Database:  "_replicator",
			ID:        doc.ID,
			Pid:       fmt.Sprintf("<%d.%d>", os.Getpid(), i),
			Source:    repDoc.Source,
			Target:    repDoc.Target,
			User:      nil,
			DocID:     doc.ID,
			History:   []string{},
			Node:      "nonode@nohost",
			StartTime: startTimeStr,
			Status:    status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"total_rows": len(jobs),
		"offset":     0,
		"jobs":       jobs,
	})
}

func replicationStateToSchedulerStatus(state model.ReplicationState) string {
	switch state {
	case model.ReplicationStateRunning:
		return "running"
	case model.ReplicationStateCompleted:
		return "completed"
	case model.ReplicationStateError, model.ReplicationStateCrashing:
		return "crashing"
	default:
		// initializing or empty
		return "added"
	}
}

type SchedulerDocs struct {
	Base
}

func (s *SchedulerDocs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"total_rows": 0,
		"offset":     0,
		"docs":       []interface{}{},
	})
}
