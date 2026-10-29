//go:build !windows

package unixhttp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
)

// Pair creates a net.Listener and an http.RoundTripper connected through
// anonymous Unix domain sockets (via socketpair(2) -- no filesystem path, no
// listening port): useful for tests or in-process embedding that want a
// real HTTP client/server round trip with zero filesystem/network
// footprint.
//
// Unlike a named socket from Listen, there is no shared listening endpoint
// -- each HTTP connection the transport opens gets its own fresh,
// private socketpair, created on demand when the transport dials and handed
// to the listener's next Accept call. There is no limit on how many such
// connections can be open concurrently; net/http's normal connection
// pooling governs how many actually get created. Closing the listener
// stops it from accepting further connections and fails any dial already
// waiting to hand one off.
func Pair() (net.Listener, http.RoundTripper, error) {
	ln := &pairListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	rt := &http.Transport{DialContext: ln.dial}
	return ln, rt, nil
}

func fdToConn(fd int, name string) (net.Conn, error) {
	f := os.NewFile(uintptr(fd), name)
	conn, err := net.FileConn(f)
	_ = f.Close() // net.FileConn dup()s the fd; the os.File wrapper is no longer needed
	if err != nil {
		return nil, fmt.Errorf("unixhttp: wrapping socketpair fd: %w", err)
	}
	return conn, nil
}

// pairListener hands out one freshly dialed socketpair connection per
// Accept call, fed by dial as the transport opens new connections.
type pairListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

// dial creates a new socketpair, keeps the client-side end for the caller
// (the http.Transport), and hands the server-side end to the next Accept
// call via conns.
func (l *pairListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("unixhttp: socketpair: %w", err)
	}

	serverConn, err := fdToConn(fds[0], "unixhttp-pair-server")
	if err != nil {
		return nil, err
	}
	clientConn, err := fdToConn(fds[1], "unixhttp-pair-client")
	if err != nil {
		_ = serverConn.Close()
		return nil, err
	}

	select {
	case l.conns <- serverConn:
		return clientConn, nil
	case <-l.closed:
		_ = serverConn.Close()
		_ = clientConn.Close()
		return nil, fmt.Errorf("unixhttp: pair listener closed")
	case <-ctx.Done():
		_ = serverConn.Close()
		_ = clientConn.Close()
		return nil, ctx.Err()
	}
}

func (l *pairListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, &net.OpError{Op: "accept", Net: "unixpair", Err: net.ErrClosed}
	}
}

func (l *pairListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pairListener) Addr() net.Addr { return pairAddr{} }

// pairAddr is a placeholder net.Addr: a pairListener has no single fixed
// endpoint, since every connection is its own independent socketpair.
type pairAddr struct{}

func (pairAddr) Network() string { return "unixpair" }
func (pairAddr) String() string  { return "unixpair" }
