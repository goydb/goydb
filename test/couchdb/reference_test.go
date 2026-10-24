// Package couchdb_test compares goydb's behaviour against a real Apache
// CouchDB server ("the reference server").
//
// The reference server is configured through environment variables, usually
// kept in the gitignored .env.couchdb file (see .env.couchdb.example) and
// loaded by `make test-couchdb`. Without GOYDB_COUCHDB_URL the comparison is
// skipped and only goydb's own behaviour is asserted.
//
// GOYDB_COUCHDB_URL points at an existing database, and the tests only create
// documents in it (with unique ids), so a database-scoped member account is
// enough — no server admin, no database creation.
package couchdb_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// client talks to one database on one server.
type client struct {
	dbURL    string // server root + database, no trailing slash
	user     string
	password string
}

// scenario is the observable outcome of the delete-then-recreate sequence from
// issue #16. Every field has to match between goydb and the reference server.
type scenario struct {
	CreateStatus   int
	CreateGen      int
	DeleteStatus   int
	TombstoneGen   int
	RecreateStatus int
	RecreateGen    int
	GetStatus      int
	GetGen         int
	StaleRevStatus int
}

func TestRecreateDeletedDocument(t *testing.T) {
	goydb, stop := startGoydb(t)
	defer stop()

	got := goydb.runScenario(t)

	// goydb's own expectation, independent of the reference server.
	assert.Equal(t, http.StatusCreated, got.RecreateStatus, "PUT without _rev over a tombstone must be accepted")
	assert.Equal(t, got.TombstoneGen+1, got.RecreateGen, "the re-created revision continues the generation count")
	assert.Equal(t, http.StatusConflict, got.StaleRevStatus, "a stale _rev over a tombstone must still conflict")

	ref := reference(t)
	if ref == nil {
		t.Skip("GOYDB_COUCHDB_URL not set, skipping the comparison against a reference CouchDB")
	}

	want := ref.runScenario(t)
	t.Logf("reference: %+v", want)
	t.Logf("goydb:     %+v", got)
	assert.Equal(t, want, got, "goydb must behave like the reference CouchDB")
}

// runScenario runs the delete-then-recreate sequence in an existing database,
// using document ids unique to this run.
func (c client) runScenario(t *testing.T) scenario {
	t.Helper()

	id := docID()
	var s scenario

	// create → delete → PUT without _rev → GET
	s.CreateStatus, s.CreateGen = c.putDoc(t, id, map[string]interface{}{"a": 1})
	s.DeleteStatus, s.TombstoneGen = c.deleteDoc(t, id, c.currentRev(t, id))
	s.RecreateStatus, s.RecreateGen = c.putDoc(t, id, map[string]interface{}{"a": 2})

	status, body := c.do(t, "GET", id, nil)
	s.GetStatus = status
	if rev, ok := body["_rev"].(string); ok {
		s.GetGen = gen(t, rev)
	}

	// create → delete → PUT with the pre-delete (stale) _rev
	staleID := docID()
	_, _ = c.putDoc(t, staleID, map[string]interface{}{"a": 1})
	staleRev := c.currentRev(t, staleID)
	_, _ = c.deleteDoc(t, staleID, staleRev)
	s.StaleRevStatus, _ = c.putDoc(t, staleID, map[string]interface{}{"a": 2, "_rev": staleRev})

	// Leave the documents deleted; a tombstone is the cheapest trace we can
	// leave behind on a shared reference server.
	if rev := c.currentRevOrEmpty(t, id); rev != "" {
		_, _ = c.deleteDoc(t, id, rev)
	}

	return s
}

func (c client) putDoc(t *testing.T, id string, doc map[string]interface{}) (status, generation int) {
	t.Helper()
	status, body := c.do(t, "PUT", id, doc)
	if rev, ok := body["rev"].(string); ok {
		generation = gen(t, rev)
	}
	return status, generation
}

func (c client) deleteDoc(t *testing.T, id, rev string) (status, generation int) {
	t.Helper()
	status, body := c.do(t, "DELETE", id+"?rev="+rev, nil)
	if newRev, ok := body["rev"].(string); ok {
		generation = gen(t, newRev)
	}
	return status, generation
}

// currentRev reads the current revision of a document.
func (c client) currentRev(t *testing.T, id string) string {
	t.Helper()
	rev := c.currentRevOrEmpty(t, id)
	require.NotEmpty(t, rev, "no _rev for %s", id)
	return rev
}

func (c client) currentRevOrEmpty(t *testing.T, id string) string {
	t.Helper()
	status, body := c.do(t, "GET", id, nil)
	if status != http.StatusOK {
		return ""
	}
	rev, _ := body["_rev"].(string)
	return rev
}

// do sends a request to a document path within the client's database.
func (c client) do(t *testing.T, method, docPath string, body interface{}) (int, map[string]interface{}) {
	t.Helper()

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, c.dbURL+"/"+docPath, r)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	// Credentials embedded in the URL are applied by http.Client itself.
	if req.URL.User == nil && c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	result := map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func gen(t *testing.T, rev string) int {
	t.Helper()
	g, _, ok := strings.Cut(rev, "-")
	require.True(t, ok, "malformed rev %q", rev)
	n, err := strconv.Atoi(g)
	require.NoError(t, err)
	return n
}

// docID returns a document id unique to this run, so repeated runs against the
// same reference database never collide with an earlier tombstone.
func docID() string {
	return fmt.Sprintf("goydb-issue16-%d", rand.Int63()) //nolint:gosec // test scratch id
}

// reference returns a client for the configured reference database, or nil.
func reference(t *testing.T) *client {
	t.Helper()

	raw := strings.TrimSuffix(os.Getenv("GOYDB_COUCHDB_URL"), "/")
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	require.NoError(t, err, "GOYDB_COUCHDB_URL")
	require.NotEmpty(t, strings.Trim(u.Path, "/"),
		"GOYDB_COUCHDB_URL must point at an existing database, e.g. https://host/goydb-test")

	return &client{
		dbURL:    raw,
		user:     os.Getenv("GOYDB_COUCHDB_USER"),
		password: os.Getenv("GOYDB_COUCHDB_PASSWORD"),
	}
}

// startGoydb runs an in-process goydb HTTP server with a single test database.
func startGoydb(t *testing.T) (c client, stop func()) {
	t.Helper()

	log := logger.NewNoLog()
	s, err := storage.Open(t.TempDir(), storage.WithLogger(log))
	require.NoError(t, err)
	require.NoError(t, s.EnsureSystemDatabases(t.Context()))

	r := mux.NewRouter()
	err = handler.Router{
		Storage:      s,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	ts := httptest.NewServer(r)
	c = client{dbURL: ts.URL + "/goydb-test", user: "admin", password: "secret"}

	// Create the database (a PUT on the database path, not a document path).
	status, _ := client{dbURL: ts.URL, user: c.user, password: c.password}.do(t, "PUT", "goydb-test", nil)
	require.Equal(t, http.StatusCreated, status, "create the in-process test database")

	return c, func() {
		ts.Close()
		_ = s.Close()
	}
}
