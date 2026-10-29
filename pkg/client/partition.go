package client

import (
	"context"
	"net/http"
	"net/url"
)

// Partition is a handle scoped to a single partition key of a partitioned
// database.
type Partition struct {
	db  *Database
	key string
}

func (p *Partition) path(suffix string) string {
	return p.db.path("/_partition/" + url.PathEscape(p.key) + suffix)
}

// Info returns metadata about the partition.
func (p *Partition) Info(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := p.db.client.do(ctx, http.MethodGet, p.path(""), nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// AllDocs returns all documents in the partition.
func (p *Partition) AllDocs(ctx context.Context, query AllDocsQuery) (*RowsResponse, error) {
	path := p.path("/_all_docs")
	if v := query.values(); len(v) > 0 {
		path += "?" + v.Encode()
	}
	var resp RowsResponse
	if err := p.db.client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Find runs a Mango query scoped to the partition.
func (p *Partition) Find(ctx context.Context, query FindQuery) (*FindResponse, error) {
	var resp FindResponse
	if err := p.db.client.do(ctx, http.MethodPost, p.path("/_find"), query, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Explain reports the query plan for a partition-scoped Mango query.
func (p *Partition) Explain(ctx context.Context, query FindQuery) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := p.db.client.do(ctx, http.MethodPost, p.path("/_explain"), query, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}
