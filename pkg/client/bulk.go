package client

import (
	"context"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

// bulkDocRequest mirrors internal/handler.BulkDocRequest's wire shape.
type bulkDocRequest struct {
	Docs     []*model.Document `json:"docs"`
	NewEdits *bool             `json:"new_edits,omitempty"`
}

// BulkDocs creates/updates/deletes multiple documents in one request. When
// newEdits is false, the documents' own revision history is trusted as-is
// (used by replication) rather than generating a new revision.
func (d *Database) BulkDocs(ctx context.Context, docs []*model.Document, newEdits bool) ([]SimpleDocResponse, error) {
	req := bulkDocRequest{Docs: docs}
	if !newEdits {
		f := false
		req.NewEdits = &f
	}

	var out []SimpleDocResponse
	if err := d.client.do(ctx, http.MethodPost, d.path("/_bulk_docs"), req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// BulkGetRequest identifies a document (optionally at specific revisions) to
// fetch via BulkGet.
type BulkGetRequest struct {
	ID   string   `json:"id"`
	Revs []string `json:"-"`
}

type bulkGetDocEntry struct {
	ID  string `json:"id"`
	Rev string `json:"rev,omitempty"`
}

type bulkGetResponse struct {
	Results []struct {
		ID   string `json:"id"`
		Docs []struct {
			OK    *model.Document `json:"ok"`
			Error map[string]any  `json:"error"`
		} `json:"docs"`
	} `json:"results"`
}

// BulkGet fetches multiple documents (optionally specific revisions of
// them) in a single request. Documents that could not be found are omitted
// from the result.
func (d *Database) BulkGet(ctx context.Context, docs []BulkGetRequest) ([]*model.Document, error) {
	var entries []bulkGetDocEntry
	for _, r := range docs {
		if len(r.Revs) == 0 {
			entries = append(entries, bulkGetDocEntry{ID: r.ID})
			continue
		}
		for _, rev := range r.Revs {
			entries = append(entries, bulkGetDocEntry{ID: r.ID, Rev: rev})
		}
	}

	var resp bulkGetResponse
	body := map[string]interface{}{"docs": entries}
	if err := d.client.do(ctx, http.MethodPost, d.path("/_bulk_get?revs=true&attachments=true"), body, &resp); err != nil {
		return nil, err
	}

	var out []*model.Document
	for _, r := range resp.Results {
		for _, doc := range r.Docs {
			if doc.OK != nil {
				out = append(out, doc.OK)
			}
		}
	}
	return out, nil
}

// RevsDiff reports, for each document ID in revs, which of the given
// revisions the server is missing.
func (d *Database) RevsDiff(ctx context.Context, revs map[string][]string) (map[string]*model.RevsDiffResult, error) {
	var out map[string]*model.RevsDiffResult
	if err := d.client.do(ctx, http.MethodPost, d.path("/_revs_diff"), revs, &out); err != nil {
		return nil, err
	}
	return out, nil
}
