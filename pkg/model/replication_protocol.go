package model

import "time"

// DBInfo contains metadata about a database.
// Used in the replication protocol's verification phase.
type DBInfo struct {
	DBName    string `json:"db_name"`
	UpdateSeq string `json:"update_seq"`
}

// ChangesResponse represents the response from a changes feed request.
type ChangesResponse struct {
	Results []ChangeResult `json:"results"`
	LastSeq string         `json:"last_seq"`
	Pending int            `json:"pending"`
}

// ChangeResult represents a single change entry in the changes feed.
type ChangeResult struct {
	Seq     string      `json:"seq"`
	ID      string      `json:"id"`
	Changes []ChangeRev `json:"changes"`
	Deleted bool        `json:"deleted,omitempty"`
	Doc     *Document   `json:"-"`
}

// ChangeRev holds a single revision string.
type ChangeRev struct {
	Rev string `json:"rev"`
}

// RevsDiffResult indicates which revisions are missing for a document.
type RevsDiffResult struct {
	Missing []string `json:"missing"`
}

// ReplicationResult holds statistics from a replication run.
type ReplicationResult struct {
	DocsRead    int
	DocsWritten int
	StartTime   time.Time
	EndTime     time.Time
}
