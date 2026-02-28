package handler

import "github.com/goydb/goydb/pkg/model"

// formatDocRow converts a single document into an AllDocs-style Rows entry.
// When doc.Key is nil (e.g. from GetDocument), it falls back to doc.ID.
func formatDocRow(doc *model.Document, includeDocs bool) Rows {
	key := doc.Key
	if key == nil {
		key = doc.ID
	}
	row := Rows{
		ID:    doc.ID,
		Key:   key,
		Value: Value{Rev: doc.Rev},
	}
	if includeDocs {
		row.Doc = doc.Data
		if row.Doc == nil {
			row.Doc = make(map[string]interface{})
		}
		row.Doc["_id"] = doc.ID
		row.Doc["_rev"] = doc.Rev
	}
	return row
}

// formatDocRows converts a slice of documents into AllDocs-style response rows.
func formatDocRows(docs []*model.Document, includeDocs bool) []Rows {
	rows := make([]Rows, len(docs))
	for i, doc := range docs {
		rows[i] = formatDocRow(doc, includeDocs)
	}
	return rows
}
