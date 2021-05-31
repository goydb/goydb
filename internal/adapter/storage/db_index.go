package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) BuildIndices(ctx context.Context, tx port.Transaction) error {
	// load all design documents
	docs, _, err := d.AllDesignDocs(ctx)
	if err != nil {
		return err
	}
	for _, doc := range docs {
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
	}
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
				err := vidx.updateSource(vf.MapFn, vf.ReduceFn)
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
		d.indices[indexName] = NewViewIndex(ddfn, vf.MapFn, vf.ReduceFn)
	}
	return nil
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
				err := sidx.updateSource(sfn.SearchFn, sfn.Analyzer)
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
		d.indices[indexName] = NewExternalSearchIndex(ddfn, sfn.SearchFn, sfn.Analyzer)
	}
	return nil
}
