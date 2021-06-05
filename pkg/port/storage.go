package port

import (
	"context"
	"io"
	"time"

	"github.com/goydb/goydb/pkg/model"
)

type AllDocsQuery struct {
	Skip      int64
	Limit     int64
	StartKey  string
	EndKey    string
	SkipLocal bool
	// view options
	DDFN        *model.DesignDocFn
	IncludeDocs bool
	ViewGroup   bool
}

type Stats struct {
	FileSize    uint64
	DocCount    uint64
	DocDelCount uint64
	Alloc       uint64
	InUse       uint64
}

type Storage interface {
	ReloadDatabases(ctx context.Context) error
	CreateDatabase(ctx context.Context, name string) (Database, error)
	DeleteDatabase(ctx context.Context, name string) error
	Databases(ctx context.Context) ([]string, error)
	Database(ctx context.Context, name string) (Database, error)
	RegisterEngine(name string, builder ViewServerBuilder) error
	String() string
	Close() error
}

type Database interface {
	Name() string
	String() string
	Sequence() string
	AllDocs(ctx context.Context, query AllDocsQuery) ([]*model.Document, int, error)
	AllDesignDocs(ctx context.Context) ([]*model.Document, int, error)
	EnrichDocuments(ctx context.Context, docs []*model.Document) error
	Changes(ctx context.Context, options *model.ChangesOptions) ([]*model.Document, int, error)
	GetAttachment(ctx context.Context, docID, name string) (*model.Attachment, error)
	DeleteAttachment(ctx context.Context, docID, name string) (string, error)
	PutAttachment(ctx context.Context, docID string, att *model.Attachment) (string, error)
	AttachmentReader(docID, attachment string) (io.ReadCloser, error)
	DocAttachment(docID, attachment string) string
	DocDir(docID string) string
	Transaction(ctx context.Context, fn func(tx Transaction) error) error
	RTransaction(ctx context.Context, fn func(tx Transaction) error) error
	PutDocument(ctx context.Context, doc *model.Document) (string, error)
	GetDocument(ctx context.Context, docID string) (*model.Document, error)
	DeleteDocument(ctx context.Context, docID, rev string) (*model.Document, error)
	FindDocs(ctx context.Context, query model.FindQuery) ([]*model.Document, *model.ExecutionStats, error)
	Iterator(ctx context.Context, ddfn *model.DesignDocFn, fn func(i Iterator) error) error
	NotifyDocumentUpdate(doc *model.Document)
	NewDocObserver(ctx context.Context) Observer
	GetSecurity(ctx context.Context) (*model.Security, error)
	PutSecurity(ctx context.Context, sec *model.Security) error
	Stats(ctx context.Context) (stats Stats, err error)
	AddTasks(ctx context.Context, tasks []*model.Task) error
	AddTasksTx(ctx context.Context, tx Transaction, tasks []*model.Task) error
	GetTasks(ctx context.Context, count int) ([]*model.Task, error)
	UpdateTask(ctx context.Context, task *model.Task) error
	PeekTasks(ctx context.Context, count int) ([]*model.Task, error)
	CompleteTasks(ctx context.Context, tasks []*model.Task) error
	TaskCount(ctx context.Context) (int, error)
	SearchDocuments(ctx context.Context, ddfn *model.DesignDocFn, sq *SearchQuery) (*SearchResult, error)
	ChangesIndex() DocumentIndex
	Indices() map[string]DocumentIndex
}

type Transaction interface {
	SetBucketName(bucket []byte)
	PutRaw(ctx context.Context, key []byte, raw interface{}) error
	PutDocument(ctx context.Context, doc *model.Document) (rev string, err error)
	GetRaw(ctx context.Context, key []byte, value interface{}) error
	GetDocument(ctx context.Context, docID string) (*model.Document, error)
	DeleteDocument(ctx context.Context, docID, rev string) (*model.Document, error)
	NextSequence() (uint64, error)
	Sequence() uint64
}

type Iterator interface {
	Total() int
	First() *model.Document
	Next() *model.Document
	IncLimit()
	Continue() bool
	Remaining() int

	SetLimit(int)
	SetStartKey([]byte)
	SetEndKey([]byte)
	SetSkip(int)
	SetSkipLocalDoc(bool)
	SetSkipDesignDoc(bool)
}

type Observer interface {
	Close()
	WaitForDoc(timeout time.Duration) *model.Document
}

type SearchQuery struct {
	Query    string
	Limit    int
	Skip     int
	Bookmark string
}

type SearchResult struct {
	Total   uint64
	Records []*SearchRecord
}

type SearchRecord struct {
	ID     string
	Order  []float64
	Fields map[string]interface{}
}
