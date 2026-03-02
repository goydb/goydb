package port

import (
	"context"
	"io"

	"github.com/goydb/goydb/pkg/model"
)

// DatabaseTx is a handle to an open write transaction on a Database.
// It embeds EngineWriteTransaction so callers can pass it wherever
// EngineWriteTransaction or EngineReadTransaction is expected.
type DatabaseTx interface {
	EngineWriteTransaction
	GetDocument(ctx context.Context, docID string) (*model.Document, error)
	PutDocument(ctx context.Context, doc *model.Document) (string, error)
	PutDocumentForReplication(ctx context.Context, doc *model.Document) error
	DeleteDocument(ctx context.Context, docID, rev string) (*model.Document, error)
	GetLeaves(ctx context.Context, docID string) ([]*model.Document, error)
	GetLeaf(ctx context.Context, docID, rev string) (*model.Document, error)
}

// Database is the high-level interface for a single CouchDB-style database.
// *storage.Database satisfies this interface.
type Database interface {
	Name() string
	Stats(ctx context.Context) (model.DatabaseStats, error)
	Compact(ctx context.Context) error
	Sequence(ctx context.Context) (string, error)

	GetDocument(ctx context.Context, docID string) (*model.Document, error)
	PutDocument(ctx context.Context, doc *model.Document) (string, error)
	DeleteDocument(ctx context.Context, docID, rev string) (*model.Document, error)
	PutDocumentForReplication(ctx context.Context, doc *model.Document) error
	GetLeaves(ctx context.Context, docID string) ([]*model.Document, error)
	GetLeaf(ctx context.Context, docID, rev string) (*model.Document, error)

	PutAttachment(ctx context.Context, docID string, att *model.Attachment) (string, error)
	GetAttachment(ctx context.Context, docID, name string) (*model.Attachment, error)
	DeleteAttachment(ctx context.Context, docID, name, rev string) (string, error)
	AttachmentReader(digest string) (io.ReadCloser, error)

	AllDocs(ctx context.Context, q AllDocsQuery) ([]*model.Document, int, error)
	AllDesignDocs(ctx context.Context) ([]*model.Document, int, error)
	FindDocs(ctx context.Context, query model.FindQuery) ([]*model.Document, *model.ExecutionStats, error)
	Changes(ctx context.Context, options *model.ChangesOptions) ([]*model.Document, int, error)

	GetSecurity(ctx context.Context) (*model.Security, error)
	PutSecurity(ctx context.Context, sec *model.Security) error

	GetRevsLimit(ctx context.Context) (int, error)
	SetRevsLimit(ctx context.Context, limit int) error
	AddListener(ctx context.Context, l ChangeListener) error
	NotifyDocumentUpdate(doc *model.Document)

	GetTasks(ctx context.Context, count int) ([]*model.Task, error)
	PeekTasks(ctx context.Context, count int) ([]*model.Task, error)
	CompleteTasks(ctx context.Context, tasks []*model.Task) error
	UpdateTask(ctx context.Context, task *model.Task) error
	TaskCount(ctx context.Context) (int, error)

	EnrichDocuments(ctx context.Context, docs []*model.Document) error

	Indices() map[string]DocumentIndex
	Iterator(ctx context.Context, ddfn *model.DesignDocFn, fn func(i Iterator) error) error
	IndexIterator(ctx context.Context, tx EngineReadTransaction, idx DocumentIndex) (Iterator, error)
	Transaction(ctx context.Context, fn func(tx DatabaseTx) error) error
	SearchDocuments(ctx context.Context, ddfn *model.DesignDocFn, sq *SearchQuery) (*SearchResult, error)
	ViewEngine(name string) ViewServerBuilder
	FilterEngine(name string) FilterServerBuilder
	ReducerEngine(name string) ReducerServerBuilder
	ValidateEngine(name string) ValidateServerBuilder
}

// Storage is the high-level interface for the storage layer that manages
// multiple databases. *storage.Storage satisfies this interface.
type Storage interface {
	Databases(ctx context.Context) ([]string, error)
	Database(ctx context.Context, name string) (Database, error)
	CreateDatabase(ctx context.Context, name string) (Database, error)
	DeleteDatabase(ctx context.Context, name string) error
	Close() error
	Path() string
}
