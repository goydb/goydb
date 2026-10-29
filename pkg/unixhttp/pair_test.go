package unixhttp_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/goydb/goydb/pkg/unixhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPair_RoundTrip(t *testing.T) {
	ln, rt, err := unixhttp.Pair()
	require.NoError(t, err)

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello-over-socketpair")
	})}
	go srv.Serve(ln) //nolint:errcheck
	defer srv.Close() //nolint:errcheck

	client := &http.Client{Transport: rt}
	resp, err := client.Get("http://unixpair/")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello-over-socketpair", string(body))
}

func TestPair_ConcurrentRequestsSucceed(t *testing.T) {
	ln, rt, err := unixhttp.Pair()
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck

	// A handler that streams forever on "/stream" (never closes its
	// response body), simulating a long-lived request like a changes feed
	// in continuous mode, while still serving ordinary requests on "/".
	unblock := make(chan struct{})
	defer close(unblock)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-unblock
			return
		}
		fmt.Fprint(w, "ok")
	})}
	go srv.Serve(ln) //nolint:errcheck
	defer srv.Close() //nolint:errcheck

	httpClient := &http.Client{Transport: rt}

	headersReceived := make(chan struct{})
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		req, _ := http.NewRequest(http.MethodGet, "http://unixpair/stream", nil)
		resp, err := httpClient.Do(req)
		close(headersReceived)
		if err != nil {
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		_, _ = io.Copy(io.Discard, resp.Body) // blocks until the handler unblocks and returns
	}()

	// Give the streaming request time to claim its own connection.
	<-headersReceived
	time.Sleep(50 * time.Millisecond)

	// A second, independent request must succeed concurrently -- each dial
	// gets its own private socketpair, so this one doesn't wait on the
	// streaming request's connection.
	resp, err := httpClient.Get("http://unixpair/")
	require.NoError(t, err, "a second request issued while the first is still open must succeed, not block")
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))

	select {
	case <-streamDone:
		t.Fatal("streaming request should still be open")
	default:
	}
}

func TestPair_CloseStopsPendingDial(t *testing.T) {
	ln, rt, err := unixhttp.Pair()
	require.NoError(t, err)

	require.NoError(t, ln.Close())

	client := &http.Client{Transport: rt}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://unixpair/", nil)
	_, err = client.Do(req)
	require.Error(t, err, "a dial attempt after Close must fail promptly, not hang")
	require.NotErrorIs(t, err, context.DeadlineExceeded, "Close should fail the dial directly instead of leaving it to time out")
}
