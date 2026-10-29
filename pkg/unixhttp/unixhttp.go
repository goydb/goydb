// Package unixhttp provides small, dependency-free helpers for using Unix
// domain sockets with net/http: a net.Listener constructor that understands
// both TCP and Unix-socket addresses, an http.RoundTripper that dials a fixed
// socket path, and a URL convention for addressing "a path on a server
// reachable via a given socket" as a single string.
//
// It has no dependency on the rest of goydb, so it can be used standalone by
// any program embedding goydb (or talking to it) that wants a plain
// *http.Client over a Unix socket, not just goydb's own replication client.
package unixhttp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Scheme is the URL scheme used to address an HTTP server reachable over a
// Unix domain socket: "http+unix://<base64url-encoded-socket-path>/<path>".
// The socket path is base64url-encoded (rather than percent-encoded) because
// it contains "/", and Go's net/url rejects a percent-encoded "/" ("%2F")
// inside the host component as it would change the URL's parsed structure.
const Scheme = "http+unix"

// ListenPrefix marks a Listen address as a Unix domain socket path rather
// than a TCP "host:port" -- e.g. "unix:///var/run/goydb.sock". Exported so
// callers (like cmd/goydb) can recognize a configured address as a Unix
// socket without duplicating the literal.
const ListenPrefix = "unix://"

// Listen creates a net.Listener for addr. addr is either a TCP "host:port"
// (e.g. ":7070", "0.0.0.0:5984") or, prefixed with "unix://", a filesystem
// path to a Unix domain socket (e.g. "unix:///var/run/goydb.sock").
func Listen(addr string) (net.Listener, error) {
	if path, ok := strings.CutPrefix(addr, ListenPrefix); ok {
		return listenUnix(path)
	}
	return net.Listen("tcp", addr)
}

// listenUnix binds a Unix domain socket at path, removing a stale socket
// file left behind by an unclean shutdown first. It refuses to remove
// anything that isn't actually a socket, to avoid destroying unrelated data.
func listenUnix(path string) (net.Listener, error) {
	if fi, err := os.Stat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("unixhttp: refusing to remove non-socket file at %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("unixhttp: removing stale socket %s: %w", path, err)
		}
	}
	return net.Listen("unix", path)
}

// Transport returns an http.RoundTripper that dials socketPath for every
// request, ignoring the request URL's own host:port.
func Transport(socketPath string) http.RoundTripper {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
}

// NewClient returns a ready-to-use *http.Client that talks to socketPath
// over a Unix domain socket.
func NewClient(socketPath string) *http.Client {
	return &http.Client{Transport: Transport(socketPath)}
}

// EncodeURL builds a "http+unix://<base64url-encoded-socket-path><path>" URL
// string -- the convention consumed by ParseURL, used e.g. to address a
// database reachable over a Unix socket as a single _replicate source/target
// string.
func EncodeURL(socketPath, path string) string {
	return Scheme + "://" + base64.RawURLEncoding.EncodeToString([]byte(socketPath)) + path
}

// ParseURL parses a Scheme-prefixed URL (as produced by EncodeURL, or
// constructed by hand, optionally with userinfo for basic auth) into the
// socket path to dial and the remaining *url.URL, with its scheme and host
// replaced by a syntactically-valid placeholder ("http" / "unix") since the
// actual dialing target is socketPath, not the host.
func ParseURL(rawURL string) (socketPath string, base *url.URL, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("unixhttp: invalid URL: %w", err)
	}
	if u.Scheme != Scheme {
		return "", nil, fmt.Errorf("unixhttp: expected scheme %q, got %q", Scheme, u.Scheme)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(u.Host)
	if err != nil {
		return "", nil, fmt.Errorf("unixhttp: invalid socket path in URL: %w", err)
	}
	socketPath = string(decoded)

	u.Scheme = "http"
	u.Host = "unix"
	return socketPath, u, nil
}
