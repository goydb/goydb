// Package client is a general-purpose Go client for goydb's (and any other
// CouchDB-compatible server's) HTTP API. Unlike pkg/replication.RemoteClient,
// which only implements the narrow replication-protocol subset, this package
// covers the full CouchDB-compatible route surface: server-level operations,
// database and document CRUD, bulk operations, attachments, changes, Mango
// find, views, security, and administrative endpoints.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/goydb/goydb/pkg/unixhttp"
)

// Client talks to a single CouchDB-compatible server.
type Client struct {
	baseURL    string
	httpClient *http.Client
	username   string
	password   string
}

// Option configures a Client created by New.
type Option func(*Client)

// WithBasicAuth sets HTTP Basic Auth credentials sent on every request.
func WithBasicAuth(username, password string) Option {
	return func(c *Client) {
		c.username = username
		c.password = password
	}
}

// WithHTTPClient overrides the *http.Client used to make requests. Useful to
// customize timeouts, or to supply a cookie jar for session-based auth ahead
// of calling CreateSession.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// New creates a Client for rawURL, which is either a normal http(s):// URL,
// or an http+unix:// URL (see pkg/unixhttp) addressing a server reachable
// over a Unix domain socket. Basic auth credentials embedded in the URL's
// userinfo are extracted automatically.
func New(rawURL string, opts ...Option) (*Client, error) {
	c := &Client{httpClient: &http.Client{}}

	var u *url.URL
	if socketPath, base, err := unixhttp.ParseURL(rawURL); err == nil {
		c.httpClient.Transport = unixhttp.Transport(socketPath)
		u = base
	} else {
		u, err = url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("client: invalid URL: %w", err)
		}
	}

	if u.User != nil {
		c.username = u.User.Username()
		c.password, _ = u.User.Password()
		u.User = nil
	}

	c.baseURL = strings.TrimSuffix(u.String(), "/")

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// DB returns a handle to the database named name. It does not verify the
// database exists -- call Create or Exists for that.
func (c *Client) DB(name string) *Database {
	return &Database{client: c, name: name}
}

// StatusError is returned for a non-2xx HTTP response, carrying the
// CouchDB-style {"error":...,"reason":...} body when the server sent one.
type StatusError struct {
	StatusCode int
	Err        string
	Reason     string
}

func (e *StatusError) Error() string {
	if e.Err != "" {
		return fmt.Sprintf("client: %d %s: %s", e.StatusCode, e.Err, e.Reason)
	}
	return fmt.Sprintf("client: unexpected status %d", e.StatusCode)
}

// IsNotFound reports whether err is a *StatusError for a 404 response.
func IsNotFound(err error) bool {
	se, ok := err.(*StatusError)
	return ok && se.StatusCode == http.StatusNotFound
}

// IsConflict reports whether err is a *StatusError for a 409 response.
func IsConflict(err error) bool {
	se, ok := err.(*StatusError)
	return ok && se.StatusCode == http.StatusConflict
}

// Raw issues a request against path (relative to the server root, e.g.
// "/mydb/mydoc") and returns the full, undecoded *http.Response -- for
// callers that need exact wire-level detail (a specific status code,
// response headers, a deliberately malformed body) that the typed methods
// don't expose. It still goes through the Client's configured transport and
// auth, unlike building a request by hand. The caller must close the
// response body. body, if non-nil, is sent as-is (not JSON-encoded) with no
// Content-Type set -- callers needing JSON must encode it and set the
// header themselves via headers.
func (c *Client) Raw(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	return c.httpClient.Do(req)
}

// request issues an HTTP request against path (relative to baseURL, e.g.
// "/mydb/mydoc"), with an optional JSON-encoded body and extra headers. The
// caller is responsible for closing the returned response's Body, and for
// checking its status code (use checkStatus for the common case).
func (c *Client) request(ctx context.Context, method, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return nil, fmt.Errorf("client: encoding request body: %w", err)
		}
		reqBody = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	return c.httpClient.Do(req)
}

// checkStatus returns a *StatusError if resp's status code is >= 300,
// consuming and decoding its body in that case; otherwise it returns nil
// and leaves the body untouched for the caller to read.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode < 300 {
		return nil
	}
	se := &StatusError{StatusCode: resp.StatusCode}
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		se.Err, se.Reason = body.Error, body.Reason
	}
	return se
}

// do issues a request, closes the response body, and decodes a JSON
// response into out (if non-nil). Returns a *StatusError for any non-2xx
// response.
func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	return c.doHeaders(ctx, method, path, nil, body, out)
}

// doHeaders is do with extra request headers (e.g. "Destination" for COPY).
func (c *Client) doHeaders(ctx context.Context, method, path string, headers map[string]string, body, out interface{}) error {
	resp, err := c.request(ctx, method, path, body, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if err := checkStatus(resp); err != nil {
		return err
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("client: decoding response: %w", err)
		}
	}
	return nil
}

// SimpleDocResponse is the {ok,id,rev} (or {ok:false,error,reason}) shape
// returned by document PUT/DELETE/bulk operations.
type SimpleDocResponse struct {
	ID     string `json:"id"`
	Ok     bool   `json:"ok"`
	Rev    string `json:"rev,omitempty"`
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// setJSONParam JSON-encodes value and sets it as query param key, if value
// is non-nil. CouchDB expects startkey/endkey/key/keys query params to be
// JSON-encoded (e.g. a string key must be quoted: "foo").
func setJSONParam(v url.Values, key string, value interface{}) {
	if value == nil {
		return
	}
	b, err := json.Marshal(value)
	if err != nil {
		return
	}
	v.Set(key, string(b))
}
