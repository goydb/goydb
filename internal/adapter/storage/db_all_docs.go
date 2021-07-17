package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) AllDocs(ctx context.Context, query port.AllDocsQuery) ([]*model.Document, int, error) {
	var total int
	var docs []*model.Document

	err := d.Iterator(ctx, query.DDFN, func(i port.Iterator) error {
		total = i.Total()
		if total == 0 {
			return nil
		}

		i.SetSkip(int(query.Skip))
		i.SetSkipLocalDoc(query.SkipLocal)
		if query.Limit != 0 {
			i.SetLimit(int(query.Limit))
		}
		if query.StartKey != "" {
			i.SetStartKey([]byte(query.StartKey))
		}
		if query.EndKey != "" {
			i.SetEndKey([]byte(query.EndKey))
		}

		for doc := i.First(); i.Continue(); doc = i.Next() {
			// TODO: handle IncludeDocs
			docs = append(docs, doc)
		}

		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	if query.DDFN != nil && query.IncludeDocs {
		err = d.EnrichDocuments(ctx, docs)
		if err != nil {
			return nil, 0, err
		}
	}

	if !query.IncludeDocs {
		for _, docs := range docs {
			docs.Data = nil
		}
	}

	return docs, total, nil
}

func (d *Database) AllDesignDocs(ctx context.Context) ([]*model.Document, int, error) {
	return d.AllDocs(ctx, port.AllDocsQuery{
		StartKey:    string(model.DesignDocPrefix),
		EndKey:      string(model.DesignDocPrefix) + "香",
		IncludeDocs: true,
	})
}

func (d *Database) EnrichDocuments(ctx context.Context, docs []*model.Document) error {
	err := d.Transaction(ctx, func(tx *Transaction) error {
		var err error
		tx.SetBucketName(model.DocsBucket)

		for _, doc := range docs {
			dbdoc, err := tx.GetDocument(ctx, doc.ID)
			if err != nil {
				return err
			}
			doc.Data = dbdoc.Data
			doc.Rev = dbdoc.Rev
			doc.Deleted = dbdoc.Deleted
			doc.Attachments = dbdoc.Attachments
		}

		return err
	})
	return err
}
