package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// AttachmentReader is a streamed attachment body. Close it when done.
type AttachmentReader struct {
	io.ReadCloser
	ContentType   string
	ContentLength int64
}

// GetAttachment fetches an attachment's content.
func (d *Database) GetAttachment(ctx context.Context, docID, name string) (*AttachmentReader, error) {
	resp, err := d.client.request(ctx, http.MethodGet, docPath(d.path(""), docID)+"/"+url.PathEscape(name), nil, nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp); err != nil {
		resp.Body.Close() //nolint:errcheck
		return nil, err
	}
	return &AttachmentReader{
		ReadCloser:    resp.Body,
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
	}, nil
}

// PutAttachment uploads an attachment's content, replacing it if it already
// exists. rev must be the document's current revision (or "" if the
// document doesn't exist yet and this call is creating it). Returns the
// document's new revision.
func (d *Database) PutAttachment(ctx context.Context, docID, name, rev, contentType string, data io.Reader) (newRev string, err error) {
	path := docPath(d.path(""), docID) + "/" + url.PathEscape(name)
	if rev != "" {
		path += "?rev=" + url.QueryEscape(rev)
	}

	buf, err := io.ReadAll(data)
	if err != nil {
		return "", fmt.Errorf("client: reading attachment data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, d.client.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	if d.client.username != "" {
		req.SetBasicAuth(d.client.username, d.client.password)
	}

	resp, err := d.client.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if err := checkStatus(resp); err != nil {
		return "", err
	}
	var out SimpleDocResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("client: decoding response: %w", err)
	}
	return out.Rev, nil
}

// DeleteAttachment removes an attachment, given the document's current
// revision. Returns the document's new revision.
func (d *Database) DeleteAttachment(ctx context.Context, docID, name, rev string) (newRev string, err error) {
	path := docPath(d.path(""), docID) + "/" + url.PathEscape(name) + "?rev=" + url.QueryEscape(rev)
	var out SimpleDocResponse
	if err := d.client.do(ctx, http.MethodDelete, path, nil, &out); err != nil {
		return "", err
	}
	return out.Rev, nil
}
