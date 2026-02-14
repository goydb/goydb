package model

import "fmt"

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
	Continuous   bool
	CreateTarget bool

	// State fields (written back to the doc)
	ReplicationState       ReplicationState
	ReplicationStateReason string
}

// ParseReplicationDoc parses a Document into a ReplicationDoc
func ParseReplicationDoc(doc *Document) (*ReplicationDoc, error) {
	if doc == nil {
		return nil, fmt.Errorf("document is nil")
	}

	source, _ := doc.Data["source"].(string)
	target, _ := doc.Data["target"].(string)
	if source == "" || target == "" {
		return nil, fmt.Errorf("source and target are required")
	}

	continuous, _ := doc.Data["continuous"].(bool)
	createTarget, _ := doc.Data["create_target"].(bool)
	state, _ := doc.Data["_replication_state"].(string)
	stateReason, _ := doc.Data["_replication_state_reason"].(string)

	return &ReplicationDoc{
		ID:                     doc.ID,
		Rev:                    doc.Rev,
		Source:                 source,
		Target:                 target,
		Continuous:             continuous,
		CreateTarget:           createTarget,
		ReplicationState:       ReplicationState(state),
		ReplicationStateReason: stateReason,
	}, nil
}

// ReplicationCheckpoint stores replication progress
type ReplicationCheckpoint struct {
	SourceLastSeq string                      `json:"source_last_seq"`
	SessionID     string                      `json:"session_id"`
	History       []ReplicationCheckpointHist `json:"history"`
}

// ReplicationCheckpointHist is a history entry in a checkpoint
type ReplicationCheckpointHist struct {
	SessionID     string `json:"session_id"`
	SourceLastSeq string `json:"source_last_seq"`
	DocsRead      int    `json:"docs_read"`
	DocsWritten   int    `json:"docs_written"`
	StartTime     string `json:"start_time"`
	EndTime       string `json:"end_time"`
}
