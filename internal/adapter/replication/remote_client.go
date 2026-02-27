package replication

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// RemoteClient implements port.ReplicationPeer for HTTP-based remote CouchDB-compatible endpoints.
type RemoteClient struct {
	baseURL       string
	username      string
	password      string
	customHeaders map[string]string
	client        *http.Client
}

var _ port.ReplicationPeer = (*RemoteClient)(nil)

// NewRemoteClient creates an HTTP client for a remote replication endpoint.
// Basic auth credentials are extracted from the URL's userinfo, or custom headers
// can be provided (e.g., Authorization header).
func NewRemoteClient(rawURL string, customHeaders map[string]string) (*RemoteClient, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	c := &RemoteClient{
		client:        &http.Client{},
		customHeaders: customHeaders,
	}

	if u.User != nil {
		c.username = u.User.Username()
		c.password, _ = u.User.Password()
		u.User = nil
	}

	// Ensure no trailing slash
	c.baseURL = u.String()
	if len(c.baseURL) > 0 && c.baseURL[len(c.baseURL)-1] == '/' {
		c.baseURL = c.baseURL[:len(c.baseURL)-1]
	}

	return c, nil
}

func (c *RemoteClient) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var reqBody *bytes.Buffer
	if body != nil {
		reqBody = &bytes.Buffer{}
		if err := json.NewEncoder(reqBody).Encode(body); err != nil {
			return nil, err
		}
	}

	var req *http.Request
	var err error
	if reqBody != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add custom headers first (can be overridden by basic auth if both are set)
	for k, v := range c.customHeaders {
		req.Header.Set(k, v)
	}

	// Basic auth from URL takes precedence over custom Authorization header
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	return c.client.Do(req)
}

func (c *RemoteClient) Head(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodHead, "", nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("database not found")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HEAD returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *RemoteClient) GetDBInfo(ctx context.Context) (*model.DBInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, "", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET db info returned status %d", resp.StatusCode)
	}

	var info model.DBInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *RemoteClient) GetLocalDoc(ctx context.Context, docID string) (*model.Document, error) {
	resp, err := c.do(ctx, http.MethodGet, "/_local/"+url.PathEscape(docID), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("local doc %q not found", docID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET _local/%s returned status %d", docID, resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	doc := &model.Document{
		Data: data,
	}
	if id, ok := data["_id"].(string); ok {
		doc.ID = id
	}
	if rev, ok := data["_rev"].(string); ok {
		doc.Rev = rev
	}
	return doc, nil
}

func (c *RemoteClient) PutLocalDoc(ctx context.Context, doc *model.Document) error {
	docID := doc.ID
	if len(docID) > len("_local/") && docID[:7] == "_local/" {
		docID = docID[7:]
	}

	resp, err := c.do(ctx, http.MethodPut, "/_local/"+url.PathEscape(docID), doc.Data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT _local/%s returned status %d: %s", docID, resp.StatusCode, body)
	}
	return nil
}

func (c *RemoteClient) GetChanges(ctx context.Context, since string, limit int) (*model.ChangesResponse, error) {
	path := fmt.Sprintf("/_changes?feed=normal&style=all_docs&heartbeat=10000&limit=%d", limit)
	if since != "" {
		path += "&since=" + url.QueryEscape(since)
	}

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET _changes returned status %d", resp.StatusCode)
	}

	var changesResp model.ChangesResponse
	if err := json.NewDecoder(resp.Body).Decode(&changesResp); err != nil {
		return nil, err
	}
	return &changesResp, nil
}

func (c *RemoteClient) RevsDiff(ctx context.Context, revs map[string][]string) (map[string]*model.RevsDiffResult, error) {
	resp, err := c.do(ctx, http.MethodPost, "/_revs_diff", revs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("POST _revs_diff returned status %d: %s", resp.StatusCode, body)
	}

	var result map[string]*model.RevsDiffResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *RemoteClient) GetDoc(ctx context.Context, docID string, revs bool, openRevs []string) (*model.Document, error) {
	path := "/" + url.PathEscape(docID)
	params := url.Values{}
	if revs {
		params.Set("revs", "true")
	}
	if len(openRevs) > 0 {
		ors, _ := json.Marshal(openRevs)
		params.Set("open_revs", string(ors))
		params.Set("attachments", "true")
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	// Build request manually when using open_revs to set Accept header
	var resp *http.Response
	var err error
	if len(openRevs) > 0 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return nil, err
		}
		// Request JSON array format instead of multipart/mixed
		req.Header.Set("Accept", "application/json")

		// Add custom headers
		for k, v := range c.customHeaders {
			req.Header.Set(k, v)
		}
		// Basic auth from URL takes precedence
		if c.username != "" {
			req.SetBasicAuth(c.username, c.password)
		}

		resp, err = c.client.Do(req)
		if err != nil {
			return nil, err
		}
	} else {
		resp, err = c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", docID, resp.StatusCode)
	}

	// When open_revs is used with Accept: application/json, CouchDB returns an array
	if len(openRevs) > 0 {
		var results []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			return nil, fmt.Errorf("failed to decode open_revs response: %w", err)
		}

		// Find the first successful result (not an error)
		for _, result := range results {
			if okData, ok := result["ok"].(map[string]interface{}); ok {
				return c.parseDocumentData(okData), nil
			}
		}
		return nil, fmt.Errorf("no successful revisions in open_revs response")
	}

	// Normal single document response
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return c.parseDocumentData(data), nil
}

// BulkGet fetches multiple documents from the remote peer using POST /_bulk_get.
// Each BulkGetRequest with multiple revisions expands into one entry per revision.
func (c *RemoteClient) BulkGet(ctx context.Context, docs []port.BulkGetRequest) ([]*model.Document, error) {
	type docEntry struct {
		ID  string `json:"id"`
		Rev string `json:"rev,omitempty"`
	}
	type bulkGetReq struct {
		Docs []docEntry `json:"docs"`
	}

	var entries []docEntry
	for _, req := range docs {
		if len(req.Revs) == 0 {
			entries = append(entries, docEntry{ID: req.ID})
		} else {
			for _, rev := range req.Revs {
				entries = append(entries, docEntry{ID: req.ID, Rev: rev})
			}
		}
	}

	resp, err := c.do(ctx, http.MethodPost, "/_bulk_get?revs=true&attachments=true", bulkGetReq{Docs: entries})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("POST _bulk_get returned status %d: %s", resp.StatusCode, body)
	}

	type bulkGetResp struct {
		Results []struct {
			ID   string `json:"id"`
			Docs []struct {
				OK    map[string]interface{} `json:"ok"`
				Error map[string]interface{} `json:"error"`
			} `json:"docs"`
		} `json:"results"`
	}

	var result bulkGetResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode _bulk_get response: %w", err)
	}

	var out []*model.Document
	for _, r := range result.Results {
		for _, d := range r.Docs {
			if d.OK != nil {
				out = append(out, c.parseDocumentData(d.OK))
			}
			// skip missing/error entries
		}
	}
	return out, nil
}

// parseDocumentData converts a map to a Document
func (c *RemoteClient) parseDocumentData(data map[string]interface{}) *model.Document {
	doc := &model.Document{
		Data: data,
	}
	if id, ok := data["_id"].(string); ok {
		doc.ID = id
	}
	if rev, ok := data["_rev"].(string); ok {
		doc.Rev = rev
	}
	if deleted, ok := data["_deleted"].(bool); ok {
		doc.Deleted = deleted
	}
	if attachments, ok := data["_attachments"].(map[string]interface{}); ok {
		doc.Attachments = make(map[string]*model.Attachment)
		for name, attData := range attachments {
			attMap, ok := attData.(map[string]interface{})
			if !ok {
				continue
			}
			att := &model.Attachment{}
			if v, ok := attMap["content_type"].(string); ok {
				att.ContentType = v
			}
			if v, ok := attMap["length"].(float64); ok {
				att.Length = int64(v)
			}
			if v, ok := attMap["stub"].(bool); ok {
				att.Stub = v
			}
			if v, ok := attMap["digest"].(string); ok {
				att.Digest = v
			}
			if v, ok := attMap["revpos"].(float64); ok {
				att.Revpos = int(v)
			}
			if v, ok := attMap["data"].(string); ok {
				att.Data = v
			}
			if v, ok := attMap["encoding"].(string); ok {
				att.Encoding = v
			}
			doc.Attachments[name] = att
		}
	}
	return doc
}

func (c *RemoteClient) BulkDocs(ctx context.Context, docs []*model.Document, newEdits bool) error {
	type bulkReq struct {
		Docs     []map[string]interface{} `json:"docs"`
		NewEdits bool                     `json:"new_edits"`
	}

	docData := make([]map[string]interface{}, len(docs))
	for i, doc := range docs {
		data := doc.Data
		if data == nil {
			data = make(map[string]interface{})
		}
		data["_id"] = doc.ID
		data["_rev"] = doc.Rev
		if doc.Deleted {
			data["_deleted"] = true
		}
		docData[i] = data
	}

	req := bulkReq{
		Docs:     docData,
		NewEdits: newEdits,
	}

	var bodyBuf bytes.Buffer
	if err := json.NewEncoder(&bodyBuf).Encode(req); err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_bulk_docs", &bodyBuf)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Couch-Full-Commit", "false")
	for k, v := range c.customHeaders {
		httpReq.Header.Set(k, v)
	}
	if c.username != "" {
		httpReq.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST _bulk_docs returned status %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (c *RemoteClient) CreateDB(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPut, "", nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusPreconditionFailed {
		return fmt.Errorf("PUT db returned status %d", resp.StatusCode)
	}
	return nil
}

// EnsureFullCommit calls the CouchDB _ensure_full_commit endpoint to flush
// pending writes to stable storage after each replication batch.
func (c *RemoteClient) EnsureFullCommit(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPost, "/_ensure_full_commit", map[string]interface{}{})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST _ensure_full_commit returned status %d: %s", resp.StatusCode, body)
	}
	return nil
}
