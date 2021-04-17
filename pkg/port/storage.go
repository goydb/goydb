package port

import (
	"context"
	"io"
	"strconv"
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
	ViewName    string
	IncludeDocs bool
	ViewGroup   bool
}

type ChangesOptions struct {
	Since   string
	Limit   int
	Timeout time.Duration
}

type Stats struct {
	FileSize uint64
	DocCount uint64
	Alloc    uint64
	InUse    uint64
}

func (o *ChangesOptions) SinceNow() bool {
	return o.Since == "now"
}

func (o *ChangesOptions) StartKey() []byte {
	if o.SinceNow() {
		return nil
	}
	sinceNo, err := strconv.ParseUint(o.Since, 10, 64)
	if err != nil {
		return nil
	}
	return []byte(model.FormatLocalSeq(sinceNo))
}

type Storage interface {
	ReloadDatabases(ctx context.Context) error
	CreateDatabase(ctx context.Context, name string) (Database, error)
	DeleteDatabase(ctx context.Context, name string) error
	Databases(ctx context.Context) ([]string, error)
	Database(ctx context.Context, name string) (Database, error)
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
	Changes(ctx context.Context, options *ChangesOptions) ([]*model.Document, int, error)
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
	Iterator(ctx context.Context, viewName string, fn func(i Iterator) error) error
	NotifyDocumentUpdate(doc *model.Document)
	NewDocObserver(ctx context.Context) Observer
	GetSecurity(ctx context.Context) (*model.Security, error)
	PutSecurity(ctx context.Context, sec *model.Security) error
	Stats(ctx context.Context) (stats Stats, err error)
	ViewSize(ctx context.Context, viewName string) (stats Stats, err error)
	AddTasks(ctx context.Context, tasks []*model.Task) error
	AddTasksTx(ctx context.Context, tx Transaction, tasks []*model.Task) error
	GetTasks(ctx context.Context, count int) ([]*model.Task, error)
	UpdateTask(ctx context.Context, task *model.Task) error
	PeekTasks(ctx context.Context, count int) ([]*model.Task, error)
	CompleteTasks(ctx context.Context, tasks []*model.Task) error
	TaskCount(ctx context.Context) (int, error)
	ResetView(ctx context.Context, name string) error
	UpdateView(ctx context.Context, name string, docs []*model.Document) error
	ResetViewIndex() error
	ResetViewIndexForDoc(ctx context.Context, docID string) error
	ChangesIndex() Index
	Indicies() []Index
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

type Index interface {
	Ensure(tx Transaction) error
	Delete(tx Transaction, doc *model.Document) error
	Put(tx Transaction, doc *model.Document) error
	Iter(tx Transaction) (Iterator, error)
}
