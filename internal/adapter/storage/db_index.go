package storage

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/goydb/goydb/internal/adapter/index"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const (
	SearchDir = "search_indices"
	indexExt  = ".bleve"
)

// BuildIndices loads all design documents and builds
// their indices
func (d *Database) BuildIndices(ctx context.Context, tx *Transaction, update bool) error {
	docs, _, err := d.AllDesignDocs(ctx)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		err = d.BuildDesignDocIndices(ctx, tx, doc, update)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Database) BuildDesignDocIndices(ctx context.Context, tx *Transaction, doc *model.Document, update bool) error {
	functions := doc.Functions()
	for _, f := range functions {
		err := d.BuildFnIndices(ctx, tx, doc, f, update)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Database) BuildFnIndices(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document, vf *model.Function, update bool) error {
	var err error

	ddfn := vf.DesignDocFn()
	indexName := ddfn.String()

	idx, ok := d.indices[indexName]
	if ok {
		// index already exists, check if update is required
		// check if already view index, update source,
		// this is only possible if the function is of the same type
		// if the type is different destroy the index and create it
		// from scratch
		disu, ok := idx.(port.DocumentIndexSourceUpdate)
		if ok && disu.SourceType() == vf.Type {
			// compile the source
			err = disu.UpdateSource(ctx, doc, vf)
			if err != nil {
				return err
			}

			// add all documents
			err = d.UpdateAllDocuments(ctx, tx, ddfn)
		} else { // otherwise remove the old index and create a new view index instead
			err = idx.Remove(ctx, tx)
		}
		if err != nil {
			return err
		}

		return nil
	}

	// index doesn't exist yet
	var disu port.DocumentIndexSourceUpdate
	switch vf.Type {
	case model.ViewFn:
		disu = index.NewViewIndex(ddfn, d.engines)
	case model.SearchFn:
		disu = index.NewExternalSearchIndex(ddfn, d.engines, d.searchIndexPath(ddfn.String()))
	// TODO: mango index
	default:
		return fmt.Errorf("invalid view function type %q for function %q", vf.Type, ddfn.String())
	}

	// create new index
	err = disu.Ensure(ctx, tx)
	if err != nil {
		return err
	}

	// compile the source
	err = disu.UpdateSource(ctx, doc, vf)
	if err != nil {
		return err
	}

	// add all documents
	if update {
		err = d.UpdateAllDocuments(ctx, tx, ddfn)
		if err != nil {
			return err
		}
	}

	// add new index
	d.indices[indexName] = disu

	return nil
}

// UpdateAllDocuments triggers rebuild with all documents
func (d *Database) UpdateAllDocuments(ctx context.Context, tx port.EngineWriteTransaction, ddfn *model.DesignDocFn) error {
	return d.AddTasksTx(ctx, tx, []*model.Task{
		{
			Action:          model.ActionUpdateView,
			DBName:          d.Name(),
			DesignDocFn:     ddfn.String(),
			ProcessingTotal: 1,
		},
	})
}

func (d *Database) SearchDocuments(ctx context.Context, ddfn *model.DesignDocFn, sq *port.SearchQuery) (*port.SearchResult, error) {
	index, ok := d.indices[ddfn.String()]
	if !ok {
		return nil, ErrNotFound
	}

	si, ok := index.(*index.ExternalSearchIndex)
	if !ok {
		return nil, fmt.Errorf("can't SearchDocuments on non search index: %q", ddfn)
	}

	return si.SearchDocuments(ctx, ddfn, sq)
}

func (d *Database) searchIndexPath(name string) string {
	return filepath.Join(d.databaseDir, SearchDir, name+indexExt)
}
