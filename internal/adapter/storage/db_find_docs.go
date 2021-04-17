package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func (d *Database) FindDocs(ctx context.Context, query model.FindQuery) ([]*model.Document, *model.ExecutionStats, error) {
	var stats model.ExecutionStats
	var total int
	var docs []*model.Document

	// total execution time
	start := time.Now()
	defer func() { stats.ExecutionTime = float64(time.Since(start)) / float64(time.Millisecond) }()

	err := d.Iterator(ctx, "", func(i port.Iterator) error {
		total = i.Total()
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
