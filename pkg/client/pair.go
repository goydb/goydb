package client

import (
	"net/http"

	"github.com/goydb/goydb/pkg/unixhttp"
)

// NewPaired starts handler on one end of a Unix socketpair (see
// pkg/unixhttp.Pair) and returns a Client wired to the other end, plus a
// close function that shuts the server down. There is no listening socket
// at all -- not a file, not a port -- useful for tests or in-process
// embedding that want a real HTTP client/server round trip with zero
// filesystem/network footprint.
//
// Each HTTP connection the returned Client opens gets its own fresh,
// private socketpair (see pkg/unixhttp.Pair), so the Client supports
// concurrent requests -- including holding a long-lived streaming request
// open (e.g. a changes feed in continuous mode) while making other calls on
// the same Client -- the same as an ordinary networked Client would.
//
// Do not pass WithHTTPClient in opts -- it would replace the paired
// transport this constructor sets up, breaking the pairing. Doing so is a
// programmer error, not a recoverable runtime condition, so NewPaired
// panics if it detects this rather than silently returning a Client that
// can't reach handler.
func NewPaired(handler http.Handler, opts ...Option) (*Client, func() error, error) {
	ln, rt, err := unixhttp.Pair()
	if err != nil {
		return nil, nil, err
	}

	srv := &http.Server{Handler: handler}
	go srv.Serve(ln) //nolint:errcheck

	pairedHTTPClient := &http.Client{Transport: rt}
	allOpts := append([]Option{WithHTTPClient(pairedHTTPClient)}, opts...)
	c, err := New("http://unixpair/", allOpts...)
	if err != nil {
		_ = srv.Close()
		return nil, nil, err
	}

	if c.httpClient != pairedHTTPClient {
		_ = srv.Close()
		panic("client: WithHTTPClient must not be passed to NewPaired -- it replaces the paired transport, breaking the connection to handler")
	}

	return c, srv.Close, nil
}
