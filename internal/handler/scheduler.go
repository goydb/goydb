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
	Database    string                   `json:"database"`
	ID          string                   `json:"id"`
	Pid         string                   `json:"pid"`
	Source      string                   `json:"source"`
	Target      string                   `json:"target"`
	User        *string                  `json:"user"`
	DocID       string                   `json:"doc_id"`
	History     []map[string]interface{} `json:"history"`
	Info        map[string]interface{}   `json:"info"`
	ErrorCount  int                      `json:"error_count"`
	Node        string                   `json:"node"`
	StartTime   string                   `json:"start_time"`
	LastUpdated string                   `json:"last_updated"`
	Status      string                   `json:"status"`
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

		// Build history entry with state transition
		history := []map[string]interface{}{
			{
				"timestamp": startTimeStr,
				"type":      status,
			},
		}

		// Build info with error details if applicable
		info := map[string]interface{}{}
		if repDoc.ReplicationStateReason != "" {
			info["error"] = repDoc.ReplicationStateReason
		}

		jobs = append(jobs, SchedulerJob{
			Database:    "_replicator",
			ID:          doc.ID,
			Pid:         fmt.Sprintf("<%d.%d>", os.Getpid(), i),
			Source:      repDoc.Source,
			Target:      repDoc.Target,
			User:        nil,
			DocID:       doc.ID,
			History:     history,
			Info:        info,
			ErrorCount:  repDoc.ConsecutiveFails,
			Node:        "nonode@nohost",
			StartTime:   startTimeStr,
			LastUpdated: startTimeStr,
			Status:      status,
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

// SchedulerDocInfo represents a replication document in the scheduler docs list.
type SchedulerDocInfo struct {
	Database    string                 `json:"database"`
	DocID       string                 `json:"doc_id"`
	ID          *string                `json:"id"`
	Node        string                 `json:"node"`
	Source      string                 `json:"source"`
	Target      string                 `json:"target"`
	State       string                 `json:"state"`
	Info        map[string]interface{} `json:"info"`
	ErrorCount  int                    `json:"error_count"`
	LastUpdated string                 `json:"last_updated"`
	StartTime   string                 `json:"start_time"`
}

type SchedulerDocs struct {
	Base
}

func (s *SchedulerDocs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	ctx := r.Context()
	schedulerDocs := make([]SchedulerDocInfo, 0)

	db, err := s.Storage.Database(ctx, "_replicator")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
			"total_rows": 0,
			"offset":     0,
			"docs":       schedulerDocs,
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

	for _, doc := range docs {
		if doc.IsDesignDoc() {
			continue
		}
		repDoc, err := model.ParseReplicationDoc(doc)
		if err != nil {
			continue
		}
		schedulerDocs = append(schedulerDocs, buildSchedulerDocInfo(doc, repDoc))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"total_rows": len(schedulerDocs),
		"offset":     0,
		"docs":       schedulerDocs,
	})
}

// SchedulerDocByID handles GET /_scheduler/docs/{replication-id}
type SchedulerDocByID struct {
	Base
}

func (s *SchedulerDocByID) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	ctx := r.Context()
	repID := pathVar(r, "repid")

	db, err := s.Storage.Database(ctx, "_replicator")
	if err != nil {
		WriteError(w, http.StatusNotFound, "replication not found")
		return
	}

	doc, err := db.GetDocument(ctx, repID)
	if err != nil {
		WriteError(w, http.StatusNotFound, "replication not found")
		return
	}

	repDoc, err := model.ParseReplicationDoc(doc)
	if err != nil {
		WriteError(w, http.StatusNotFound, "replication not found")
		return
	}

	info := buildSchedulerDocInfo(doc, repDoc)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info) // nolint: errcheck
}

func buildSchedulerDocInfo(doc *model.Document, repDoc *model.ReplicationDoc) SchedulerDocInfo {
	state := string(repDoc.ReplicationState)
	if state == "" {
		state = "initializing"
	}

	stateTime := time.Time{}
	if t, ok := doc.Data["_replication_state_time"].(string); ok && t != "" {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			stateTime = parsed
		}
	}
	timeStr := stateTime.UTC().Format(time.RFC3339)

	info := map[string]interface{}{}
	if repDoc.ReplicationStateReason != "" {
		info["error"] = repDoc.ReplicationStateReason
	}

	return SchedulerDocInfo{
		Database:    "_replicator",
		DocID:       doc.ID,
		ID:          nil,
		Node:        "nonode@nohost",
		Source:      repDoc.Source,
		Target:      repDoc.Target,
		State:       state,
		Info:        info,
		ErrorCount:  repDoc.ConsecutiveFails,
		LastUpdated: timeStr,
		StartTime:   timeStr,
	}
}
