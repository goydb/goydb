package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Row is a single row of an _all_docs, _design_docs, or view response.
type Row struct {
	ID    string                 `json:"id,omitempty"`
	Key   interface{}            `json:"key,omitempty"`
	Value interface{}            `json:"value"`
	Doc   map[string]interface{} `json:"doc,omitempty"`
	Error string                 `json:"error,omitempty"`
}

// RowsResponse is the common {total_rows,offset,rows} shape of _all_docs,
// _design_docs, and view responses.
type RowsResponse struct {
	TotalRows int   `json:"total_rows"`
	Offset    int   `json:"offset"`
	Rows      []Row `json:"rows"`
}

// AllDocsQuery configures a Database.AllDocs call.
type AllDocsQuery struct {
	StartKey     interface{}
	EndKey       interface{}
	Key          interface{}
	Keys         []string
	Limit        int
	Skip         int
	Descending   bool
	IncludeDocs  bool
	InclusiveEnd *bool
}

func (q AllDocsQuery) values() url.Values {
	v := url.Values{}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Skip > 0 {
		v.Set("skip", strconv.Itoa(q.Skip))
	}
	if q.Descending {
		v.Set("descending", "true")
	}
	if q.IncludeDocs {
		v.Set("include_docs", "true")
	}
	if q.InclusiveEnd != nil {
		v.Set("inclusive_end", boolStr(*q.InclusiveEnd))
	}
	setJSONParam(v, "startkey", q.StartKey)
	setJSONParam(v, "endkey", q.EndKey)
	setJSONParam(v, "key", q.Key)
	return v
}

// AllDocs returns all documents in the database (or a selected range/subset
// of them, per query).
func (d *Database) AllDocs(ctx context.Context, query AllDocsQuery) (*RowsResponse, error) {
	path := d.path("/_all_docs")
	var resp RowsResponse
	var err error
	if len(query.Keys) > 0 {
		err = d.client.do(ctx, http.MethodPost, path+"?"+query.values().Encode(), map[string]interface{}{"keys": query.Keys}, &resp)
	} else {
		v := query.values()
		if len(v) > 0 {
			path += "?" + v.Encode()
		}
		err = d.client.do(ctx, http.MethodGet, path, nil, &resp)
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// DesignDocs returns all design documents in the database.
func (d *Database) DesignDocs(ctx context.Context, query AllDocsQuery) (*RowsResponse, error) {
	path := d.path("/_design_docs")
	v := query.values()
	if len(v) > 0 {
		path += "?" + v.Encode()
	}
	var resp RowsResponse
	if err := d.client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
