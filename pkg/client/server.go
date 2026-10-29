package client

import (
	"context"
	"net/http"
	"sort"
	"strconv"
)

// Up checks server liveness (GET /_up).
func (c *Client) Up(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/_up", nil, nil)
}

// AllDBs returns the names of all databases on the server.
func (c *Client) AllDBs(ctx context.Context) ([]string, error) {
	var names []string
	if err := c.do(ctx, http.MethodGet, "/_all_dbs", nil, &names); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// DBsInfo returns info for multiple databases by name in one request. A
// database that doesn't exist (or is unavailable) is omitted from errors
// and instead reported inline in the returned map, matching CouchDB's
// {"key":...,"error":...} shape for that entry.
func (c *Client) DBsInfo(ctx context.Context, names []string) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	body := map[string]interface{}{"keys": names}
	if err := c.do(ctx, http.MethodPost, "/_dbs_info", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UUIDs requests count freshly generated UUIDs from the server.
func (c *Client) UUIDs(ctx context.Context, count int) ([]string, error) {
	path := "/_uuids"
	if count > 0 {
		path += "?count=" + strconv.Itoa(count)
	}
	var resp struct {
		UUIDs []string `json:"uuids"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.UUIDs, nil
}

// Membership returns cluster membership info (single-node: just this node).
func (c *Client) Membership(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_membership", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ActiveTasks returns currently running background tasks.
func (c *Client) ActiveTasks(ctx context.Context) ([]map[string]interface{}, error) {
	var resp []map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_active_tasks", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DBUpdates returns the feed of database creation/deletion events.
func (c *Client) DBUpdates(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_db_updates", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}
