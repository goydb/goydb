package client

import (
	"context"
	"net/http"
)

// GetClusterSetup returns cluster setup status.
func (c *Client) GetClusterSetup(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_cluster_setup", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// PostClusterSetup configures cluster setup (single-node no-op, accepted
// for CouchDB compatibility).
func (c *Client) PostClusterSetup(ctx context.Context, body map[string]interface{}) error {
	return c.do(ctx, http.MethodPost, "/_cluster_setup", body, nil)
}
