package port

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

// BulkGetRequest identifies one document (and specific revisions) to fetch in a batch.
type BulkGetRequest struct {
	ID   string
	Revs []string // specific revisions needed (from RevsDiff); empty = current rev
}

// ReplicationPeer represents a CouchDB-compatible database endpoint for replication.
// It abstracts both local storage (via LocalDB adapter) and remote HTTP endpoints
// (via RemoteClient adapter), enabling the Replicator to work uniformly with either.
//
// This interface follows the CouchDB replication protocol:
// https://docs.couchdb.org/en/stable/replication/protocol.html
type ReplicationPeer interface {
	// Head checks that the database exists
	Head(ctx context.Context) error

	// GetDBInfo returns database metadata (name, update sequence)
	GetDBInfo(ctx context.Context) (*model.DBInfo, error)

	// GetLocalDoc retrieves a _local document (used for checkpoints)
	// Checkpoint documents don't replicate
	GetLocalDoc(ctx context.Context, docID string) (*model.Document, error)

	// PutLocalDoc stores a _local document
	PutLocalDoc(ctx context.Context, doc *model.Document) error

	// GetChanges returns changes since the given sequence
	// Get documents that changed since `since` sequence
	GetChanges(ctx context.Context, since string, limit int) (*model.ChangesResponse, error)

	// RevsDiff returns which revisions are missing on this peer
	// Determine which revisions are missing on target
	RevsDiff(ctx context.Context, revs map[string][]string) (map[string]*model.RevsDiffResult, error)

	// GetDoc retrieves a document, optionally with revision history
	// Fetch document with specific revisions
	GetDoc(ctx context.Context, docID string, revs bool, openRevs []string) (*model.Document, error)

	// BulkGet fetches multiple documents in a single call.
	// For remote peers this maps to POST /_bulk_get; local peers iterate GetDoc.
	// attachments=true is implied: callers should expect inline base64 data.
	BulkGet(ctx context.Context, docs []BulkGetRequest) ([]*model.Document, error)

	// BulkDocs writes documents; when newEdits is false, revisions are preserved
	// Bulk write documents
	BulkDocs(ctx context.Context, docs []*model.Document, newEdits bool) error

	// CreateDB creates the database (for create_target option)
	CreateDB(ctx context.Context) error

	// EnsureFullCommit requests the peer to flush all pending writes to stable
	// storage. Called after each BulkDocs batch for CouchDB protocol compliance.
	// Local peers implement this as a no-op; remote peers call _ensure_full_commit.
	EnsureFullCommit(ctx context.Context) error
}
