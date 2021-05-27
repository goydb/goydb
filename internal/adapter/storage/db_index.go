package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// DocumentIndex
//
// Each document that is processed in the database is processed by a
// set of document indices. These indices are updating either sync or
// async depending on their type and or configuration.
//
// This interface is common to all indices and used by the database to
// have a common interface.
//
// The content of the index is accessed using the Iterator.
type DocumentIndex interface {
	// InitIndex initializes the index, this might mean, that files
	// buckets or other structures ar build or opened.
	InitIndex(ctx context.Context) error

	// Rebuild rebuilds the complete index by going over all documents
	// and rebuild the index
	Rebuild(ctx context.Context) error

	// Removes the index and all related data, do document
	// is removed only the index
	Remove(ctx context.Context) error

	// Stats returns statistics related to the index that give
	// insight about number of documents, number of records, used space.
	Stats(ctx context.Context) (*IndexStats, error)

	// DocumentCreated is called in the context of
	// the transaction that is adding a document.
	DocumentCreated(ctx context.Context, tx port.Transaction, doc *model.Document) error

	// DocumentUpdated is called in the context of
	// the tranaction that is updating the document.
	DocumentUpdated(ctx context.Context, tx port.Transaction, doc *model.Document) error

	// DocumentDeleted is called in the context of
	// the transaction that is deleting the document.
	// This call can be called multiple times.
	DocumentDeleted(ctx context.Context, tx port.Transaction, doc *model.Document) error

	// Iterator provides an iterator to the index
	// using the passed transaction context.
	Iterator(ctx context.Context, tx port.Transaction) (port.Iterator, error)
}

// IndexStats
//
// Since an index may have multiple records pointing to the same document
// or may ignore documents, the number of Records may be higher than the
// number of Documents.
type IndexStats struct {
	// Documents number of document in the index
	Documents uint64
	// Size number of bytes used by the index
	Size uint64
	// Number of records (keys)
	Records uint64
}
