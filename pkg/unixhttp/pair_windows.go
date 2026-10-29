//go:build windows

package unixhttp

import (
	"errors"
	"net"
	"net/http"
)

// Pair is unsupported on Windows: syscall.Socketpair is not available there.
func Pair() (net.Listener, http.RoundTripper, error) {
	return nil, nil, errors.New("unixhttp: Pair is not supported on Windows")
}
