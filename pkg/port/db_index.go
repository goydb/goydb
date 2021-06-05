package port

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
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
	// Ensure initializes the index, this might mean, that files
	// buckets or other structures ar build or opened.
	Ensure(ctx context.Context, tx Transaction) error

	// Removes the index and all related data, do document
	// is removed only the index
	Remove(ctx context.Context, tx Transaction) error

	// Stats returns statistics related to the index that give
	// insight about number of documents, number of records, used space.
	Stats(ctx context.Context, tx Transaction) (*model.IndexStats, error)

	// DocumentUpdated is called in the context of
	// the tranaction that is updating the document.
	DocumentStored(ctx context.Context, tx Transaction, doc *model.Document) error

	// UpdateStored is called in the context of
	// an index update with a number of documents.
	UpdateStored(ctx context.Context, tx Transaction, docs []*model.Document) error

	// DocumentDeleted is called in the context of
	// the transaction that is deleting the document.
	// This call can be called multiple times.
	DocumentDeleted(ctx context.Context, tx Transaction, doc *model.Document) error

	// Iterator provides an iterator to the index
	// using the passed transaction context.
	Iterator(ctx context.Context, tx Transaction) (Iterator, error)
}

// DocumentIndexSourceUpdate is implemented by indices that are
// based on design documents
type DocumentIndexSourceUpdate interface {
	DocumentIndex

	// UpdateSource update the source code base for the given index using the
	// passed design document and function
	UpdateSource(ctx context.Context, doc *model.Document, f *model.Function) error

	// returns the source type of the index
	SourceType() model.FnType
}
