package unixhttp_test

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/goydb/goydb/pkg/unixhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListen_TCP(t *testing.T) {
	ln, err := unixhttp.Listen("127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	assert.Equal(t, "tcp", ln.Addr().Network())
}

func TestListen_Unix(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "goydb.sock")

	ln, err := unixhttp.Listen("unix://" + sockPath)
	require.NoError(t, err)
	defer ln.Close()

	assert.Equal(t, "unix", ln.Addr().Network())
	assert.Equal(t, sockPath, ln.Addr().String())
}

func TestListen_Unix_RemovesStaleSocket(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "goydb.sock")

	// Simulate a socket file left behind by an unclean shutdown: bind a
	// listener, then close it WITHOUT unlinking the file (a clean
	// net.UnixListener.Close normally removes it itself).
	ln1, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	ul, ok := ln1.(*net.UnixListener)
	require.True(t, ok)
	ul.SetUnlinkOnClose(false)
	require.NoError(t, ln1.Close())

	_, statErr := os.Stat(sockPath)
	require.NoError(t, statErr, "test setup: stale socket file must still exist")

	ln2, err := unixhttp.Listen("unix://" + sockPath)
	require.NoError(t, err, "Listen must clean up a stale socket file and succeed")
	defer ln2.Close()

	assert.Equal(t, "unix", ln2.Addr().Network())
}

func TestListen_Unix_RefusesNonSocketFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notasocket")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o600))

	_, err := unixhttp.Listen("unix://" + path)
	require.Error(t, err)
}

func TestEncodeURL_ParseURL_RoundTrip(t *testing.T) {
	socketPath := "/var/run/goydb.sock"
	raw := unixhttp.EncodeURL(socketPath, "/mydb")

	gotPath, base, err := unixhttp.ParseURL(raw)
	require.NoError(t, err)
	assert.Equal(t, socketPath, gotPath)
	assert.Equal(t, "http", base.Scheme)
	assert.Equal(t, "unix", base.Host)
	assert.Equal(t, "/mydb", base.Path)
}

func TestParseURL_PreservesUserinfo(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("/var/run/goydb.sock"))
	raw := "http+unix://admin:secret@" + encoded + "/mydb"

	socketPath, base, err := unixhttp.ParseURL(raw)
	require.NoError(t, err)
	assert.Equal(t, "/var/run/goydb.sock", socketPath)
	require.NotNil(t, base.User)
	assert.Equal(t, "admin", base.User.Username())
	pw, ok := base.User.Password()
	assert.True(t, ok)
	assert.Equal(t, "secret", pw)
}

func TestParseURL_RejectsOtherSchemes(t *testing.T) {
	_, _, err := unixhttp.ParseURL("http://example.com/mydb")
	require.Error(t, err)
}

func TestTransport_DialsUnixSocket(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "goydb.sock")

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello-over-unix-socket")
	}))
	require.NoError(t, ts.Listener.Close())
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	ts.Listener = ln
	ts.Start()
	defer ts.Close()

	client := unixhttp.NewClient(sockPath)
	resp, err := client.Get("http://unix/")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello-over-unix-socket", string(body))
}
