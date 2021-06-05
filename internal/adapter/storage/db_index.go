package storage

import (
	"context"
	"fmt"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// BuildIndices loads all design documents and builds
// their indices
func (d *Database) BuildIndices(ctx context.Context, tx port.Transaction) error {
	docs, _, err := d.AllDesignDocs(ctx)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		err = d.BuildDesignDocIndices(ctx, tx, doc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Database) BuildDesignDocIndices(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	functions := doc.Functions()
	for _, f := range functions {
		err := d.BuildFnIndices(ctx, tx, doc, f)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Database) BuildFnIndices(ctx context.Context, tx port.Transaction, doc *model.Document, vf *model.Function) error {
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
		disu = NewViewIndex(ddfn, d.engines)
	case model.SearchFn:
		disu = NewExternalSearchIndex(ddfn, d.engines)
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
	err = d.UpdateAllDocuments(ctx, tx, ddfn)
	if err != nil {
		return err
	}

	// add new index
	d.indices[indexName] = disu

	return nil
}

// UpdateAllDocuments triggers rebuild with all documents
func (d *Database) UpdateAllDocuments(ctx context.Context, tx port.Transaction, ddfn *model.DesignDocFn) error {
	return d.AddTasksTx(ctx, tx, []*model.Task{
		{
			Action:          model.ActionUpdateView,
			DBName:          d.Name(),
			DesignDocFn:     ddfn.String(),
			ProcessingTotal: 1,
		},
	})
}
