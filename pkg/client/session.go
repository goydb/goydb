package client

import (
	"context"
	"net/http"
	"net/http/cookiejar"
)

// SessionInfo is the response of POST /_session.
type SessionInfo struct {
	Ok    bool     `json:"ok"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

// CreateSession authenticates via POST /_session (cookie-based auth) and
// retains the resulting session cookie for subsequent requests. It installs
// a cookie jar on the underlying *http.Client if one isn't already set.
func (c *Client) CreateSession(ctx context.Context, name, password string) (*SessionInfo, error) {
	if c.httpClient.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, err
		}
		c.httpClient.Jar = jar
	}

	body := map[string]string{"name": name, "password": password}
	var info SessionInfo
	if err := c.do(ctx, http.MethodPost, "/_session", body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetSession returns info about the current session (cookie or Basic Auth).
func (c *Client) GetSession(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_session", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteSession logs out the current cookie-based session.
func (c *Client) DeleteSession(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/_session", nil, nil)
}
