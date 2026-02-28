package handler

import (
	"testing"

	"github.com/goydb/goydb/pkg/model"
)

func TestFormatDocRow(t *testing.T) {
	t.Run("basic without include_docs", func(t *testing.T) {
		doc := &model.Document{
			ID:  "doc1",
			Rev: "1-abc",
			Key: "doc1",
			Data: map[string]interface{}{
				"foo": "bar",
			},
		}
		row := formatDocRow(doc, false)

		if row.ID != "doc1" {
			t.Errorf("expected ID %q, got %q", "doc1", row.ID)
		}
		if row.Key != "doc1" {
			t.Errorf("expected Key %q, got %v", "doc1", row.Key)
		}
		v, ok := row.Value.(Value)
		if !ok || v.Rev != "1-abc" {
			t.Errorf("expected Value.Rev %q, got %v", "1-abc", row.Value)
		}
		if row.Doc != nil {
			t.Errorf("expected Doc to be nil when includeDocs=false, got %v", row.Doc)
		}
	})

	t.Run("with include_docs", func(t *testing.T) {
		doc := &model.Document{
			ID:  "doc2",
			Rev: "2-def",
			Key: "doc2",
			Data: map[string]interface{}{
				"name": "test",
			},
		}
		row := formatDocRow(doc, true)

		if row.Doc == nil {
			t.Fatal("expected Doc to be populated when includeDocs=true")
		}
		if row.Doc["_id"] != "doc2" {
			t.Errorf("expected Doc._id %q, got %v", "doc2", row.Doc["_id"])
		}
		if row.Doc["_rev"] != "2-def" {
			t.Errorf("expected Doc._rev %q, got %v", "2-def", row.Doc["_rev"])
		}
		if row.Doc["name"] != "test" {
			t.Errorf("expected Doc.name %q, got %v", "test", row.Doc["name"])
		}
	})

	t.Run("nil Key falls back to ID", func(t *testing.T) {
		doc := &model.Document{
			ID:  "doc3",
			Rev: "1-ghi",
		}
		row := formatDocRow(doc, false)

		if row.Key != "doc3" {
			t.Errorf("expected Key to fall back to ID %q, got %v", "doc3", row.Key)
		}
	})

	t.Run("nil Data with include_docs creates empty map", func(t *testing.T) {
		doc := &model.Document{
			ID:  "doc4",
			Rev: "1-jkl",
			Key: "doc4",
		}
		row := formatDocRow(doc, true)

		if row.Doc == nil {
			t.Fatal("expected Doc to be non-nil even when Data is nil")
		}
		if row.Doc["_id"] != "doc4" {
			t.Errorf("expected Doc._id %q, got %v", "doc4", row.Doc["_id"])
		}
		if row.Doc["_rev"] != "1-jkl" {
			t.Errorf("expected Doc._rev %q, got %v", "1-jkl", row.Doc["_rev"])
		}
	})
}

func TestFormatDocRows(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		rows := formatDocRows(nil, false)
		if len(rows) != 0 {
			t.Errorf("expected 0 rows, got %d", len(rows))
		}
	})

	t.Run("multiple docs", func(t *testing.T) {
		docs := []*model.Document{
			{ID: "a", Rev: "1-a", Key: "a"},
			{ID: "b", Rev: "1-b", Key: "b"},
			{ID: "c", Rev: "1-c"}, // nil Key
		}
		rows := formatDocRows(docs, false)

		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(rows))
		}
		if rows[0].ID != "a" {
			t.Errorf("rows[0].ID = %q, want %q", rows[0].ID, "a")
		}
		if rows[2].Key != "c" {
			t.Errorf("rows[2].Key = %v, want %q (nil fallback to ID)", rows[2].Key, "c")
		}
	})

	t.Run("with include_docs", func(t *testing.T) {
		docs := []*model.Document{
			{ID: "x", Rev: "1-x", Key: "x", Data: map[string]interface{}{"val": 1}},
		}
		rows := formatDocRows(docs, true)

		if rows[0].Doc == nil {
			t.Fatal("expected Doc to be populated")
		}
		if rows[0].Doc["_id"] != "x" {
			t.Errorf("expected _id %q, got %v", "x", rows[0].Doc["_id"])
		}
	})
}
