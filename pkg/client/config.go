package client

import (
	"context"
	"net/http"
	"net/url"
)

// GetConfigSection returns all key/value pairs in a /_config section.
func (c *Client) GetConfigSection(ctx context.Context, section string) (map[string]string, error) {
	var out map[string]string
	if err := c.do(ctx, http.MethodGet, "/_config/"+url.PathEscape(section), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetConfig returns a single /_config/{section}/{key} value.
func (c *Client) GetConfig(ctx context.Context, section, key string) (string, error) {
	var out string
	err := c.do(ctx, http.MethodGet, "/_config/"+url.PathEscape(section)+"/"+url.PathEscape(key), nil, &out)
	return out, err
}

// PutConfig sets a /_config/{section}/{key} value.
func (c *Client) PutConfig(ctx context.Context, section, key, value string) error {
	return c.do(ctx, http.MethodPut, "/_config/"+url.PathEscape(section)+"/"+url.PathEscape(key), value, nil)
}

// DeleteConfig removes a /_config/{section}/{key} value.
func (c *Client) DeleteConfig(ctx context.Context, section, key string) error {
	return c.do(ctx, http.MethodDelete, "/_config/"+url.PathEscape(section)+"/"+url.PathEscape(key), nil, nil)
}
