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
//
// All HTTP calls here go through pkg/client, goydb's general-purpose Go
// client for the CouchDB-compatible HTTP API -- this file doubles as an
// integration test for that client, including against a real external
// CouchDB server, not just goydb's own.
package couchdb_test

import (
	"errors"
	"fmt"
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
	pkgclient "github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ref talks to one database on one server via pkg/client.
type ref struct {
	db *pkgclient.Database
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

	refServer := reference(t)
	if refServer == nil {
		t.Skip("GOYDB_COUCHDB_URL not set, skipping the comparison against a reference CouchDB")
	}

	want := refServer.runScenario(t)
	t.Logf("reference: %+v", want)
	t.Logf("goydb:     %+v", got)
	assert.Equal(t, want, got, "goydb must behave like the reference CouchDB")
}

// runScenario runs the delete-then-recreate sequence in an existing database,
// using document ids unique to this run.
func (r ref) runScenario(t *testing.T) scenario {
	t.Helper()

	id := docID()
	var s scenario

	// create → delete → PUT without _rev → GET
	s.CreateStatus, s.CreateGen = r.putDoc(t, id, "", map[string]interface{}{"a": 1})
	s.DeleteStatus, s.TombstoneGen = r.deleteDoc(t, id, r.currentRev(t, id))
	s.RecreateStatus, s.RecreateGen = r.putDoc(t, id, "", map[string]interface{}{"a": 2})

	doc, err := r.db.Get(t.Context(), id)
	s.GetStatus = statusOf(t, err, http.StatusOK)
	if err == nil {
		s.GetGen = gen(t, doc.Rev)
	}

	// create → delete → PUT with the pre-delete (stale) _rev
	staleID := docID()
	_, _ = r.putDoc(t, staleID, "", map[string]interface{}{"a": 1})
	staleRev := r.currentRev(t, staleID)
	_, _ = r.deleteDoc(t, staleID, staleRev)
	s.StaleRevStatus, _ = r.putDoc(t, staleID, staleRev, map[string]interface{}{"a": 2})

	// Leave the documents deleted; a tombstone is the cheapest trace we can
	// leave behind on a shared reference server.
	if rev := r.currentRevOrEmpty(t, id); rev != "" {
		_, _ = r.deleteDoc(t, id, rev)
	}

	return s
}

// putDoc creates or updates a document. rev is "" for a create (or an
// intentionally stale/wrong revision for a conflict test); it is NOT read
// back from data.
func (r ref) putDoc(t *testing.T, id, rev string, data map[string]interface{}) (status, generation int) {
	t.Helper()
	newRev, err := r.db.Put(t.Context(), &model.Document{ID: id, Rev: rev, Data: data})
	status = statusOf(t, err, http.StatusCreated)
	if newRev != "" {
		generation = gen(t, newRev)
	}
	return status, generation
}

func (r ref) deleteDoc(t *testing.T, id, rev string) (status, generation int) {
	t.Helper()
	newRev, err := r.db.Delete(t.Context(), id, rev)
	status = statusOf(t, err, http.StatusOK)
	if newRev != "" {
		generation = gen(t, newRev)
	}
	return status, generation
}

// currentRev reads the current revision of a document.
func (r ref) currentRev(t *testing.T, id string) string {
	t.Helper()
	rev := r.currentRevOrEmpty(t, id)
	require.NotEmpty(t, rev, "no _rev for %s", id)
	return rev
}

func (r ref) currentRevOrEmpty(t *testing.T, id string) string {
	t.Helper()
	doc, err := r.db.Get(t.Context(), id)
	if err != nil {
		return ""
	}
	return doc.Rev
}

// statusOf derives the HTTP status code implied by a pkg/client call's
// outcome: okStatus on success (the client doesn't surface the exact 2xx
// code CouchDB used), or the *pkgclient.StatusError's code on failure.
func statusOf(t *testing.T, err error, okStatus int) int {
	t.Helper()
	if err == nil {
		return okStatus
	}
	var se *pkgclient.StatusError
	if errors.As(err, &se) {
		return se.StatusCode
	}
	t.Fatalf("unexpected transport error: %v", err)
	return 0
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

// reference returns a ref for the configured reference database, or nil.
func reference(t *testing.T) *ref {
	t.Helper()

	raw := strings.TrimSuffix(os.Getenv("GOYDB_COUCHDB_URL"), "/")
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	require.NoError(t, err, "GOYDB_COUCHDB_URL")
	dbName := strings.Trim(u.Path, "/")
	require.NotEmpty(t, dbName,
		"GOYDB_COUCHDB_URL must point at an existing database, e.g. https://host/goydb-test")

	serverURL := *u
	serverURL.Path = ""
	serverURL.RawPath = ""

	c, err := pkgclient.New(serverURL.String(), pkgclient.WithBasicAuth(
		os.Getenv("GOYDB_COUCHDB_USER"), os.Getenv("GOYDB_COUCHDB_PASSWORD"),
	))
	require.NoError(t, err)

	return &ref{db: c.DB(dbName)}
}

// startGoydb runs an in-process goydb HTTP server with a single test database.
func startGoydb(t *testing.T) (r ref, stop func()) {
	t.Helper()

	log := logger.NewNoLog()
	s, err := storage.Open(t.TempDir(), storage.WithLogger(log))
	require.NoError(t, err)
	require.NoError(t, s.EnsureSystemDatabases(t.Context()))

	router := mux.NewRouter()
	err = handler.Router{
		Storage:      s,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(router)
	require.NoError(t, err)

	ts := httptest.NewServer(router)

	c, err := pkgclient.New(ts.URL, pkgclient.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	db := c.DB("goydb-test")
	require.NoError(t, db.Create(t.Context()), "create the in-process test database")

	return ref{db: db}, func() {
		ts.Close()
		_ = s.Close()
	}
}
