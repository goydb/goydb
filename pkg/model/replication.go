package model

import (
	"fmt"
	"time"
)

// ReplicationState represents the state of a replication job
type ReplicationState string

const (
	ReplicationStateInitializing ReplicationState = "initializing"
	ReplicationStateRunning      ReplicationState = "running"
	ReplicationStateCompleted    ReplicationState = "completed"
	ReplicationStateError        ReplicationState = "error"
	ReplicationStateCrashing     ReplicationState = "crashing"
)

// ReplicationDoc represents a document in the _replicator database
type ReplicationDoc struct {
	ID           string
	Rev          string
	Source       string
	Target       string
	SourceHeaders map[string]string // Custom headers for source endpoint
	TargetHeaders map[string]string // Custom headers for target endpoint
	Continuous   bool
	CreateTarget bool
	DocIDs       []string
	Filter       string
	Selector     map[string]interface{}

	// State fields (written back to the doc)
	ReplicationState       ReplicationState
	ReplicationStateReason string

	// Retry metadata
	ConsecutiveFails int
	LastRetryTime    time.Time
}

// parseEndpoint extracts a URL string and optional headers from a replication
// endpoint value, which may be a plain string or an object of the form
// {"url": "...", "headers": {...}}.
func parseEndpoint(v interface{}) (string, map[string]string) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		url, _ := m["url"].(string)

		var headers map[string]string
		if h, ok := m["headers"].(map[string]interface{}); ok {
			headers = make(map[string]string)
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}

		return url, headers
	}
	return "", nil
}

// ParseReplicationDoc parses a Document into a ReplicationDoc
func ParseReplicationDoc(doc *Document) (*ReplicationDoc, error) {
	if doc == nil {
		return nil, fmt.Errorf("document is nil")
	}

	source, sourceHeaders := parseEndpoint(doc.Data["source"])
	target, targetHeaders := parseEndpoint(doc.Data["target"])
	if source == "" || target == "" {
		return nil, fmt.Errorf("source and target are required")
	}

	continuous, _ := doc.Data["continuous"].(bool)
	createTarget, _ := doc.Data["create_target"].(bool)
	filter, _ := doc.Data["filter"].(string)

	var docIDs []string
	if ids, ok := doc.Data["doc_ids"].([]interface{}); ok {
		for _, id := range ids {
			if s, ok := id.(string); ok {
				docIDs = append(docIDs, s)
			}
		}
	}

	var selector map[string]interface{}
	if sel, ok := doc.Data["selector"].(map[string]interface{}); ok {
		selector = sel
	}

	state, _ := doc.Data["_replication_state"].(string)
	stateReason, _ := doc.Data["_replication_state_reason"].(string)

	// Parse retry metadata
	var consecutiveFails int
	var lastRetryTime time.Time
	if fails, ok := doc.Data["_replication_consecutive_fails"].(float64); ok {
		consecutiveFails = int(fails)
	}
	if retryTime, ok := doc.Data["_replication_last_retry"].(string); ok {
		t, err := time.Parse(time.RFC3339, retryTime)
		if err == nil {
			lastRetryTime = t
		}
	}

	return &ReplicationDoc{
		ID:                     doc.ID,
		Rev:                    doc.Rev,
		Source:                 source,
		Target:                 target,
		SourceHeaders:          sourceHeaders,
		TargetHeaders:          targetHeaders,
		Continuous:             continuous,
		CreateTarget:           createTarget,
		DocIDs:                 docIDs,
		Filter:                 filter,
		Selector:               selector,
		ReplicationState:       ReplicationState(state),
		ReplicationStateReason: stateReason,
		ConsecutiveFails:       consecutiveFails,
		LastRetryTime:          lastRetryTime,
	}, nil
}

// ReplicationCheckpoint stores replication progress
type ReplicationCheckpoint struct {
	ReplicationIDVersion int                         `json:"replication_id_version"`
	SourceLastSeq        string                      `json:"source_last_seq"`
	SessionID            string                      `json:"session_id"`
	History              []ReplicationCheckpointHist `json:"history"`
}

// ReplicationCheckpointHist is a history entry in a checkpoint
type ReplicationCheckpointHist struct {
	SessionID       string `json:"session_id"`
	SourceLastSeq   string `json:"source_last_seq"`
	DocsRead        int    `json:"docs_read"`
	DocsWritten     int    `json:"docs_written"`
	DocWriteFailures int   `json:"doc_write_failures"`
	MissingFound    int    `json:"missing_found"`
	MissingChecked  int    `json:"missing_checked"`
	StartLastSeq    string `json:"start_last_seq"`
	EndLastSeq      string `json:"end_last_seq"`
	RecordedSeq     string `json:"recorded_seq"`
	StartTime       string `json:"start_time"`
	EndTime         string `json:"end_time"`
}
