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
)

// Client implements Peer for a remote CouchDB-compatible database via HTTP
type Client struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

var _ Peer = (*Client)(nil)

// NewClient creates a remote peer from a URL. Basic auth is extracted from userinfo.
func NewClient(rawURL string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	c := &Client{
		client: &http.Client{},
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

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
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
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	return c.client.Do(req)
}

func (c *Client) Head(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodHead, "", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("database not found")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HEAD returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GetDBInfo(ctx context.Context) (*DBInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET db info returned status %d", resp.StatusCode)
	}

	var info DBInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) GetLocalDoc(ctx context.Context, docID string) (*model.Document, error) {
	resp, err := c.do(ctx, http.MethodGet, "/_local/"+url.PathEscape(docID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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

func (c *Client) PutLocalDoc(ctx context.Context, doc *model.Document) error {
	docID := doc.ID
	if len(docID) > len("_local/") && docID[:7] == "_local/" {
		docID = docID[7:]
	}

	resp, err := c.do(ctx, http.MethodPut, "/_local/"+url.PathEscape(docID), doc.Data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT _local/%s returned status %d: %s", docID, resp.StatusCode, body)
	}
	return nil
}

func (c *Client) GetChanges(ctx context.Context, since string, limit int) (*ChangesResponse, error) {
	path := fmt.Sprintf("/_changes?limit=%d", limit)
	if since != "" {
		path += "&since=" + url.QueryEscape(since)
	}

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET _changes returned status %d", resp.StatusCode)
	}

	var changesResp ChangesResponse
	if err := json.NewDecoder(resp.Body).Decode(&changesResp); err != nil {
		return nil, err
	}
	return &changesResp, nil
}

func (c *Client) RevsDiff(ctx context.Context, revs map[string][]string) (map[string]*RevsDiffResult, error) {
	resp, err := c.do(ctx, http.MethodPost, "/_revs_diff", revs)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("POST _revs_diff returned status %d: %s", resp.StatusCode, body)
	}

	var result map[string]*RevsDiffResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) GetDoc(ctx context.Context, docID string, revs bool, openRevs []string) (*model.Document, error) {
	path := "/" + url.PathEscape(docID)
	params := url.Values{}
	if revs {
		params.Set("revs", "true")
	}
	if len(openRevs) > 0 {
		ors, _ := json.Marshal(openRevs)
		params.Set("open_revs", string(ors))
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", docID, resp.StatusCode)
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
	if deleted, ok := data["_deleted"].(bool); ok {
		doc.Deleted = deleted
	}

	return doc, nil
}

func (c *Client) BulkDocs(ctx context.Context, docs []*model.Document, newEdits bool) error {
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

	resp, err := c.do(ctx, http.MethodPost, "/_bulk_docs", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST _bulk_docs returned status %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) CreateDB(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPut, "", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusPreconditionFailed {
		return fmt.Errorf("PUT db returned status %d", resp.StatusCode)
	}
	return nil
}
