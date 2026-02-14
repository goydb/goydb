package replication

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

// DBInfo contains database metadata
type DBInfo struct {
	DBName    string `json:"db_name"`
	UpdateSeq string `json:"update_seq"`
}

// ChangesResponse represents a changes feed response
type ChangesResponse struct {
	Results []ChangeResult `json:"results"`
	LastSeq string         `json:"last_seq"`
	Pending int            `json:"pending"`
}

// ChangeResult is a single change entry
type ChangeResult struct {
	Seq     string       `json:"seq"`
	ID      string       `json:"id"`
	Changes []ChangeRev  `json:"changes"`
	Deleted bool         `json:"deleted,omitempty"`
	Doc     *model.Document `json:"-"`
}

// ChangeRev holds a revision string
type ChangeRev struct {
	Rev string `json:"rev"`
}

// RevsDiffResult contains the missing and possible ancestor revisions
type RevsDiffResult struct {
	Missing []string `json:"missing"`
}

// Peer abstracts a database endpoint (local or remote) for replication
type Peer interface {
	// Head checks that the database exists
	Head(ctx context.Context) error
	// GetDBInfo returns database metadata
	GetDBInfo(ctx context.Context) (*DBInfo, error)
	// GetLocalDoc retrieves a _local document (used for checkpoints)
	GetLocalDoc(ctx context.Context, docID string) (*model.Document, error)
	// PutLocalDoc stores a _local document
	PutLocalDoc(ctx context.Context, doc *model.Document) error
	// GetChanges returns changes since the given sequence
	GetChanges(ctx context.Context, since string, limit int) (*ChangesResponse, error)
	// RevsDiff returns which revisions are missing on this peer
	RevsDiff(ctx context.Context, revs map[string][]string) (map[string]*RevsDiffResult, error)
	// GetDoc retrieves a document, optionally with revision history
	GetDoc(ctx context.Context, docID string, revs bool, openRevs []string) (*model.Document, error)
	// BulkDocs writes documents; when newEdits is false, revisions are preserved
	BulkDocs(ctx context.Context, docs []*model.Document, newEdits bool) error
	// CreateDB creates the database
	CreateDB(ctx context.Context) error
}
