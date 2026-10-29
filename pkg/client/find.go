package client

import (
	"context"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

// FindQuery is a Mango query request. Unlike model.FindQuery (which only
// implements UnmarshalJSON, for decoding the server's incoming request
// body), Selector here is a plain JSON-marshalable map, so a FindQuery built
// by client code actually serializes back into valid Mango selector syntax
// on the wire.
type FindQuery struct {
	Selector       map[string]interface{} `json:"selector"`
	Sort           []map[string]string     `json:"sort,omitempty"`
	Fields         []string                `json:"fields,omitempty"`
	Limit          int                     `json:"limit,omitempty"`
	Skip           int                     `json:"skip,omitempty"`
	Bookmark       string                  `json:"bookmark,omitempty"`
	ExecutionStats bool                    `json:"execution_stats,omitempty"`
	UseIndex       interface{}             `json:"use_index,omitempty"`
}

// FindResponse is the response of _find.
type FindResponse struct {
	Docs           []map[string]interface{} `json:"docs"`
	Bookmark       string                   `json:"bookmark,omitempty"`
	ExecutionStats *model.ExecutionStats    `json:"execution_stats,omitempty"`
	Warning        string                   `json:"warning,omitempty"`
}

// Find runs a Mango query.
func (d *Database) Find(ctx context.Context, query FindQuery) (*FindResponse, error) {
	var resp FindResponse
	if err := d.client.do(ctx, http.MethodPost, d.path("/_find"), query, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Explain reports the query plan CouchDB/goydb would use for query, without
// executing it.
func (d *Database) Explain(ctx context.Context, query FindQuery) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := d.client.do(ctx, http.MethodPost, d.path("/_explain"), query, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// IndexDef describes a Mango index to create via CreateIndex.
type IndexDef struct {
	Index map[string]interface{} `json:"index"`
	Ddoc  string                 `json:"ddoc,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Type  string                 `json:"type,omitempty"`
}

// CreateIndex creates a Mango index.
func (d *Database) CreateIndex(ctx context.Context, idx IndexDef) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := d.client.do(ctx, http.MethodPost, d.path("/_index"), idx, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ListIndexes returns all Mango indexes defined on the database.
func (d *Database) ListIndexes(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := d.client.do(ctx, http.MethodGet, d.path("/_index"), nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteIndex removes a Mango index.
func (d *Database) DeleteIndex(ctx context.Context, ddoc, name string) error {
	return d.client.do(ctx, http.MethodDelete, d.path("/_index/"+ddoc+"/json/"+name), nil, nil)
}
