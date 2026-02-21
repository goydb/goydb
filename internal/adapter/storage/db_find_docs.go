package storage

import (
	"context"
	"fmt"
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

	// Attempt index-based shortcut for equality conditions.
	if eqFields := query.EqConditions(); len(eqFields) > 0 {
		if mi, values := d.bestMangoIndex(eqFields); mi != nil {
			return d.findDocsViaIndex(ctx, query, mi, values, &stats, start)
		}
	}

	// Full-table scan fallback.
	err := d.Iterator(ctx, nil, func(i port.Iterator) error {
		total := i.Total()
		if total == 0 {
			return nil
		}

		i.SetSkip(int(query.Skip))
		if query.Limit != 0 {
			i.SetLimit(int(query.Limit))
		}
		if query.Bookmark != "" {
			i.SetStartKey([]byte(query.Bookmark))
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
			} else {
				i.IncLimit()
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return docs, &stats, nil
}

// bestMangoIndex finds the first MangoIndex whose fields are a prefix of (or
// exactly match) the equality conditions. Returns the index and ordered values
// that cover its fields, or (nil, nil) if no suitable index exists.
func (d *Database) bestMangoIndex(eqFields map[string]interface{}) (*index.MangoIndex, []interface{}) {
	for _, idx := range d.indices {
		mi, ok := idx.(*index.MangoIndex)
		if !ok {
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
