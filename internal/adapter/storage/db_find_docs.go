package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goydb/goydb/internal/adapter/index"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) FindDocs(ctx context.Context, query model.FindQuery) ([]*model.Document, *model.ExecutionStats, error) {
	var stats model.ExecutionStats
	var docs []*model.Document

	// total execution time
	start := time.Now()
	defer func() { stats.ExecutionTime = float64(time.Since(start)) / float64(time.Millisecond) }()

	hasSort := len(query.Sort) > 0

	// Attempt index-based shortcut for equality conditions (only when no sort requested).
	if !hasSort {
		if eqFields := query.EqConditions(); len(eqFields) > 0 {
			if mi, values := d.bestMangoIndex(query); mi != nil {
				return d.findDocsViaIndex(ctx, query, mi, values, &stats, start)
			}
		}
	}

	// Full-table scan fallback.
	err := d.Iterator(ctx, nil, func(i port.Iterator) error {
		total := i.Total()
		if total == 0 {
			return nil
		}

		if !hasSort {
			i.SetSkip(int(query.Skip))
			if query.Limit != 0 {
				i.SetLimit(int(query.Limit))
			}
			if query.Bookmark != "" {
				i.SetStartKey([]byte(query.Bookmark))
			}
		}

		for doc := i.First(); i.Continue(); doc = i.Next() {
			stats.TotalDocsExamined++

			ok, err := query.Match(doc)
			if err != nil {
				return fmt.Errorf("find failed: %w", err)
			}
			if ok {
				stats.ResultsReturned++
				docs = append(docs, doc)
			} else if !hasSort {
				i.IncLimit()
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if hasSort {
		query.SortDocuments(docs)
		skip := query.Skip
		if skip > len(docs) {
			skip = len(docs)
		}
		docs = docs[skip:]
		limit := query.Limit
		if limit > 0 && limit < len(docs) {
			docs = docs[:limit]
		}
		stats.ResultsReturned = len(docs)
	}

	return docs, &stats, nil
}

// bestMangoIndex finds the first MangoIndex whose fields are a prefix of (or
// exactly match) the equality conditions. Returns the index and ordered values
// that cover its fields, or (nil, nil) if no suitable index exists.
// If query.UseIndex is set it acts as a hint to prefer a specific index.
func (d *Database) bestMangoIndex(query model.FindQuery) (*index.MangoIndex, []interface{}) {
	eqFields := query.EqConditions()

	// Parse use_index hint. Index keys have the format "<fnType>:<ddoc>:<name>".
	var wantPrefix string
	switch ui := query.UseIndex.(type) {
	case string:
		wantPrefix = string(model.MangoFn) + ":" + ui + ":"
	case []interface{}:
		if len(ui) >= 1 {
			wantPrefix = string(model.MangoFn) + ":" + fmt.Sprint(ui[0])
			if len(ui) >= 2 {
				wantPrefix += ":" + fmt.Sprint(ui[1])
			} else {
				wantPrefix += ":"
			}
		}
	}

	for key, idx := range d.indices {
		mi, ok := idx.(*index.MangoIndex)
		if !ok {
			continue
		}
		if wantPrefix != "" && !strings.HasPrefix(key, wantPrefix) {
			continue
		}
		fields := mi.Fields()
		if len(fields) == 0 {
			continue
		}
		// Check that every index field has an equality condition.
		values := make([]interface{}, len(fields))
		covered := true
		for i, f := range fields {
			v, ok := eqFields[f]
			if !ok {
				covered = false
				break
			}
			values[i] = v
		}
		if covered {
			return mi, values
		}
	}
	return nil, nil
}

// findDocsViaIndex performs an equality lookup using a MangoIndex and loads
// matching documents from the database.
func (d *Database) findDocsViaIndex(
	ctx context.Context,
	query model.FindQuery,
	mi *index.MangoIndex,
	values []interface{},
	stats *model.ExecutionStats,
	_ time.Time,
) ([]*model.Document, *model.ExecutionStats, error) {
	var ids []string

	err := d.rawTx(func(tx *Transaction) error {
		var err error
		ids, err = mi.LookupEq(ctx, tx, values)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	skip := query.Skip
	limit := query.Limit
	if limit == 0 {
		limit = -1 // no limit
	}

	var docs []*model.Document
	for _, id := range ids {
		doc, err := d.GetDocument(ctx, id)
		if err != nil || doc == nil || doc.Deleted {
			continue
		}
		stats.TotalDocsExamined++
		stats.TotalKeysExamined++

		ok, err := query.Match(doc)
		if err != nil {
			return nil, nil, fmt.Errorf("find failed: %w", err)
		}
		if !ok {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		stats.ResultsReturned++
		docs = append(docs, doc)
		if limit > 0 && len(docs) >= limit {
			break
		}
	}

	return docs, stats, nil
}
