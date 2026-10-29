package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/goydb/goydb/pkg/model"
)

// DocOption configures a Database.Get call via query parameters.
type DocOption func(url.Values)

// WithRev requests a specific document revision.
func WithRev(rev string) DocOption {
	return func(v url.Values) { v.Set("rev", rev) }
}

// WithRevs includes the full revision history in the response.
func WithRevs(include bool) DocOption {
	return func(v url.Values) { v.Set("revs", boolStr(include)) }
}

// WithConflicts includes conflicting revisions in the response.
func WithConflicts(include bool) DocOption {
	return func(v url.Values) { v.Set("conflicts", boolStr(include)) }
}

// WithAttachments includes attachment bodies (base64-encoded) in the response.
func WithAttachments(include bool) DocOption {
	return func(v url.Values) { v.Set("attachments", boolStr(include)) }
}

// WithLatest, when true, returns the latest leaf revision of a conflicted
// document instead of erroring when the requested rev is not a leaf.
func WithLatest(latest bool) DocOption {
	return func(v url.Values) { v.Set("latest", boolStr(latest)) }
}

// docPath joins a database path with a document ID, correctly escaping IDs
// that themselves contain a "/". Only the genuine "_design/" and "_local/"
// prefixes are split into their own path segment (CouchDB's route template
// has a fixed "_design"/"_local" segment followed by the document name); any
// other slash in docID is part of a plain document ID and is escaped as one
// opaque path segment (url.PathEscape already percent-encodes embedded "/",
// so e.g. "a/b/c" becomes "a%2Fb%2Fc", not three segments).
func docPath(dbPath, docID string) string {
	for _, prefix := range []string{string(model.DesignDocPrefix), string(model.LocalDocPrefix)} {
		if rest, ok := strings.CutPrefix(docID, prefix); ok {
			name := strings.TrimSuffix(prefix, "/")
			return dbPath + "/" + url.PathEscape(name) + "/" + url.PathEscape(rest)
		}
	}
	return dbPath + "/" + url.PathEscape(docID)
}

// Get fetches a document by ID, which may be a design ("_design/x") or
// local ("_local/x") document ID.
func (d *Database) Get(ctx context.Context, docID string, opts ...DocOption) (*model.Document, error) {
	v := url.Values{}
	for _, opt := range opts {
		opt(v)
	}
	path := docPath(d.path(""), docID)
	if len(v) > 0 {
		path += "?" + v.Encode()
	}

	var doc model.Document
	if err := d.client.do(ctx, http.MethodGet, path, nil, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Put creates or updates a document. doc.ID must be set; doc.Rev must match
// the current revision when updating an existing document. Returns the new
// revision.
func (d *Database) Put(ctx context.Context, doc *model.Document) (rev string, err error) {
	if doc.ID == "" {
		return "", fmt.Errorf("client: document ID is required")
	}

	body := make(map[string]interface{}, len(doc.Data)+1)
	for k, v := range doc.Data {
		body[k] = v
	}
	if doc.Rev != "" {
		body["_rev"] = doc.Rev
	}
	if doc.Deleted {
		body["_deleted"] = true
	}

	var resp SimpleDocResponse
	if err := d.client.do(ctx, http.MethodPut, docPath(d.path(""), doc.ID), body, &resp); err != nil {
		return "", err
	}
	return resp.Rev, nil
}

// Delete marks the document identified by docID (at revision rev) as
// deleted. Returns the tombstone's revision.
func (d *Database) Delete(ctx context.Context, docID, rev string) (newRev string, err error) {
	path := docPath(d.path(""), docID) + "?rev=" + url.QueryEscape(rev)
	var resp SimpleDocResponse
	if err := d.client.do(ctx, http.MethodDelete, path, nil, &resp); err != nil {
		return "", err
	}
	return resp.Rev, nil
}

// Head returns the current revision of a document without fetching its
// body, or "" if the document doesn't exist.
func (d *Database) Head(ctx context.Context, docID string) (rev string, err error) {
	resp, err := d.client.request(ctx, http.MethodHead, docPath(d.path(""), docID), nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if err := checkStatus(resp); err != nil {
		return "", err
	}
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// Copy copies the document at srcID (at srcRev, or its current revision if
// srcRev is "") to destID, replacing destID's existing revision destRev (if
// given). Returns destID's new revision.
func (d *Database) Copy(ctx context.Context, srcID, srcRev, destID, destRev string) (newRev string, err error) {
	path := docPath(d.path(""), srcID)
	if srcRev != "" {
		path += "?rev=" + url.QueryEscape(srcRev)
	}

	dest := destID
	if destRev != "" {
		dest += "?rev=" + destRev
	}

	var resp SimpleDocResponse
	if err := d.client.doHeaders(ctx, "COPY", path, map[string]string{"Destination": dest}, nil, &resp); err != nil {
		return "", err
	}
	return resp.Rev, nil
}
