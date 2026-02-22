package controller

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// MangoIndexCreateReq is the request body for POST /{db}/_index.
type MangoIndexCreateReq struct {
	Index struct {
		Fields []string `json:"fields"`
	} `json:"index"`
	Ddoc string `json:"ddoc"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// MangoIndexResult is the response for POST /{db}/_index.
type MangoIndexResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MangoIndex controller handles business logic for the three _index HTTP operations.
type MangoIndex struct {
	DB port.Database
}

// Create creates or verifies a Mango index definition in a design document.
// Returns (result, created, error) where created=true means a new index was stored.
func (c MangoIndex) Create(ctx context.Context, req MangoIndexCreateReq) (*MangoIndexResult, bool, error) {
	fields := req.Index.Fields
	if len(fields) == 0 {
		return nil, false, fmt.Errorf("index must have at least one field")
	}

	// Generate ddoc/name from fields if not supplied.
	ddocName := req.Ddoc
	idxName := req.Name
	if ddocName == "" || idxName == "" {
		fieldsJSON, _ := json.Marshal(fields)
		h := sha1.Sum(fieldsJSON)
		generated := "mango_idx_" + hex.EncodeToString(h[:4])
		if ddocName == "" {
			ddocName = generated
		}
		if idxName == "" {
			idxName = generated
		}
	}

	ddocID := string(model.DesignDocPrefix) + ddocName

	// Load existing design doc (may not exist — that's fine).
	doc, err := c.DB.GetDocument(ctx, ddocID)
	if err != nil {
		return nil, false, fmt.Errorf("loading design doc: %w", err)
	}
	if doc == nil {
		doc = &model.Document{
			ID:   ddocID,
			Data: map[string]interface{}{},
		}
	}

	// Check for existing identical index.
	if existing, ok := doc.MangoIndex(idxName); ok {
		if fieldsEqual(existing.Fields, fields) {
			return &MangoIndexResult{ID: ddocID, Name: idxName}, false, nil
		}
	}

	// Merge index definition into design doc.
	mangoIndexes, _ := doc.Data["mango_indexes"].(map[string]interface{})
	if mangoIndexes == nil {
		mangoIndexes = make(map[string]interface{})
	}
	mangoIndexes[idxName] = map[string]interface{}{
		"fields": fieldsToInterface(fields),
	}
	doc.Data["mango_indexes"] = mangoIndexes

	_, err = c.DB.PutDocument(ctx, doc)
	if err != nil {
		return nil, false, fmt.Errorf("saving design doc: %w", err)
	}

	return &MangoIndexResult{ID: ddocID, Name: idxName}, true, nil
}

// List returns all Mango indexes across all design documents, prepended by
// the built-in _all_docs special index.
func (c MangoIndex) List(ctx context.Context) ([]*model.MangoIndex, error) {
	docs, _, err := c.DB.AllDesignDocs(ctx)
	if err != nil {
		return nil, err
	}

	// Built-in special index first.
	indexes := []*model.MangoIndex{
		{Name: "_all_docs", Ddoc: "", Fields: []string{"_id"}},
	}

	for _, doc := range docs {
		indexes = append(indexes, doc.MangoIndexes()...)
	}

	return indexes, nil
}

// Delete removes a named Mango index from a design document.
func (c MangoIndex) Delete(ctx context.Context, ddoc, name string) error {
	// Normalise ddoc — strip leading "_design/" if caller already included it.
	ddocID := ddoc
	if !strings.HasPrefix(ddocID, string(model.DesignDocPrefix)) {
		ddocID = string(model.DesignDocPrefix) + ddoc
	}

	doc, err := c.DB.GetDocument(ctx, ddocID)
	if err != nil {
		return fmt.Errorf("loading design doc: %w", err)
	}
	if doc == nil {
		return fmt.Errorf("design document %q not found", ddocID)
	}

	if _, ok := doc.MangoIndex(name); !ok {
		return fmt.Errorf("index %q not found in %q", name, ddocID)
	}

	mangoIndexes, _ := doc.Data["mango_indexes"].(map[string]interface{})
	delete(mangoIndexes, name)
	doc.Data["mango_indexes"] = mangoIndexes

	// If the design doc has no remaining functions, delete it.
	if len(doc.Functions()) == 0 {
		rev, _ := doc.Revision()
		_, err = c.DB.DeleteDocument(ctx, ddocID, rev)
		return err
	}

	_, err = c.DB.PutDocument(ctx, doc)
	return err
}

// fieldsEqual reports whether two string slices have the same elements in the same order.
func fieldsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fieldsToInterface(fields []string) []interface{} {
	out := make([]interface{}, len(fields))
	for i, f := range fields {
		out[i] = f
	}
	return out
}
