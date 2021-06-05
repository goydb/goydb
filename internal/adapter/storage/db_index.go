package storage

import (
	"context"

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
	// view index
	err := d.BuildViewIndices(ctx, tx, doc)
	if err != nil {
		return err
	}

	// search index
	err = d.BuildSearchIndices(ctx, tx, doc)
	if err != nil {
		return err
	}

	// mango index
	// TODO:

	return nil
}

func (d *Database) BuildViewIndices(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	vfns := doc.ViewFunctions()
	for _, vf := range vfns {
		ddfn := &model.DesignDocFn{
			Type:        model.ViewFn,
			DesignDocID: doc.ID,
			FnName:      vf.Name,
		}
		indexName := ddfn.String()

		idx, ok := d.indices[indexName]
		if ok {
			// index already exists, check if update is required
			// check if already view index, update source
			vidx, ok := idx.(*ViewIndex)
			if ok {
				err := vidx.updateSource(doc.Language(), vf.MapFn)
				if err != nil {
					return err
				}
				err = d.UpdateAllDocuments(ctx, tx, ddfn)
				if err != nil {
					return err
				}
				continue
			} else { // otherwise remove the old index and create a new view index instead
				err := idx.Remove(ctx, tx)
				if err != nil {
					return err
				}
				continue
			}
		}

		// index doesn't exist yet
		vidx := NewViewIndex(ddfn, d.engines)
		err := vidx.updateSource(doc.Language(), vf.MapFn)
		if err != nil {
			return err
		}
		err = d.UpdateAllDocuments(ctx, tx, ddfn)
		if err != nil {
			return err
		}
		d.indices[indexName] = vidx
	}
	return nil
}

// UpdateAllDocuments triggers rebuild with all documents
func (d *Database) UpdateAllDocuments(ctx context.Context, tx port.Transaction, ddfn *model.DesignDocFn) error {
	return d.AddTasksTx(ctx, tx, []*model.Task{
		{
			Action:          model.ActionUpdateView,
			DBName:          d.Name(),
			Ddfn:            ddfn.String(),
			ProcessingTotal: 1,
		},
	})
}

func (d *Database) BuildSearchIndices(ctx context.Context, tx port.Transaction, doc *model.Document) error {
	sfns := doc.SearchFunctions()
	for _, sfn := range sfns {
		ddfn := &model.DesignDocFn{
			Type:        model.ViewFn,
			DesignDocID: doc.ID,
			FnName:      sfn.Name,
		}
		indexName := ddfn.String()

		idx, ok := d.indices[indexName]
		if ok {
			// index already exists, check if update is required
			// check if already view index, update source
			sidx, ok := idx.(*ExternalSearchIndex)
			if ok {
				err := sidx.updateSource(doc.Language(), sfn.SearchFn, sfn.Analyzer)
				if err != nil {
					return err
				}
				continue
			} else { // otherwise remove the old index and create a new view index instead
				err := idx.Remove(ctx, tx)
				if err != nil {
					return err
				}
			}
		}

		// index doesn't exist yet
		sidx := NewExternalSearchIndex(ddfn, sfn.SearchFn, sfn.Analyzer, d.engines)
		err := sidx.updateSource(doc.Language(), sfn.SearchFn, sfn.Analyzer)
		if err != nil {
			return err
		}
		d.indices[indexName] = sidx
	}
	return nil
}
