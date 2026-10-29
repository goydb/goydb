package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/unixhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestServer spins up a real in-process goydb HTTP server (TCP) and
// returns a *client.Client authenticated against it as admin/secret.
func setupTestServer(t *testing.T) *client.Client {
	t.Helper()

	log := logger.NewNoLog()
	stor, err := storage.Open(t.TempDir(),
		storage.WithLogger(log),
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
		storage.WithReducerEngine("", gojaview.NewReducerBuilder(log)),
		storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(log)),
	)
	require.NoError(t, err)
	require.NoError(t, stor.EnsureSystemDatabases(context.Background()))

	r := mux.NewRouter()
	err = handler.Router{
		Storage:      stor,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: stor, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	// View/index building is driven by the background task controller, not
	// synchronously inline with writes -- without it, a _view request (which
	// polls for its index to be built) would block indefinitely.
	taskCtx, cancelTasks := context.WithCancel(context.Background())
	tc := controller.Task{Storage: stor, Logger: log}
	go tc.Run(taskCtx)
	t.Cleanup(cancelTasks)

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = stor.Close() })

	c, err := client.New(ts.URL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	return c
}

// setupUnixTestServer is setupTestServer but reached over a Unix domain
// socket, proving pkg/client composes correctly with pkg/unixhttp.
func setupUnixTestServer(t *testing.T) *client.Client {
	t.Helper()

	log := logger.NewNoLog()
	stor, err := storage.Open(t.TempDir(), storage.WithLogger(log))
	require.NoError(t, err)
	require.NoError(t, stor.EnsureSystemDatabases(context.Background()))

	r := mux.NewRouter()
	err = handler.Router{
		Storage:      stor,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: stor, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	sockPath := filepath.Join(t.TempDir(), "goydb.sock")
	ts := httptest.NewUnstartedServer(r)
	require.NoError(t, ts.Listener.Close())
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = stor.Close() })

	rawURL := unixhttp.EncodeURL(sockPath, "")
	c, err := client.New(rawURL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	return c
}

func TestClient_Up(t *testing.T) {
	c := setupTestServer(t)
	require.NoError(t, c.Up(context.Background()))
}

func TestClient_DatabaseLifecycle(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")

	exists, err := db.Exists(ctx)
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, db.Create(ctx))

	exists, err = db.Exists(ctx)
	require.NoError(t, err)
	require.True(t, exists)

	info, err := db.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, "testdb", info.DBName)

	names, err := c.AllDBs(ctx)
	require.NoError(t, err)
	require.Contains(t, names, "testdb")

	require.NoError(t, db.Destroy(ctx))

	exists, err = db.Exists(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestClient_DocumentCRUD(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	rev, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"hello": "world"}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	doc, err := db.Get(ctx, "doc1")
	require.NoError(t, err)
	require.Equal(t, "doc1", doc.ID)
	require.Equal(t, rev, doc.Rev)
	require.Equal(t, "world", doc.Data["hello"])

	headRev, err := db.Head(ctx, "doc1")
	require.NoError(t, err)
	require.Equal(t, rev, headRev)

	doc.Data["hello"] = "updated"
	rev2, err := db.Put(ctx, doc)
	require.NoError(t, err)
	require.NotEqual(t, rev, rev2)

	newRev, err := db.Delete(ctx, "doc1", rev2)
	require.NoError(t, err)
	require.NotEmpty(t, newRev)

	_, err = db.Get(ctx, "doc1")
	require.True(t, client.IsNotFound(err))
}

func TestClient_DocumentCopy(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	_, err := db.Put(ctx, &model.Document{ID: "src", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	newRev, err := db.Copy(ctx, "src", "", "dest", "")
	require.NoError(t, err)
	require.NotEmpty(t, newRev)

	dest, err := db.Get(ctx, "dest")
	require.NoError(t, err)
	require.EqualValues(t, 1, dest.Data["n"])
}

func TestClient_Attachments(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	rev, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	rev2, err := db.PutAttachment(ctx, "doc1", "hello.txt", rev, "text/plain", strings.NewReader("hello world"))
	require.NoError(t, err)
	require.NotEmpty(t, rev2)

	att, err := db.GetAttachment(ctx, "doc1", "hello.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(att)
	require.NoError(t, err)
	require.NoError(t, att.Close())
	require.Equal(t, "hello world", string(data))
	require.Equal(t, "text/plain", att.ContentType)

	rev3, err := db.DeleteAttachment(ctx, "doc1", "hello.txt", rev2)
	require.NoError(t, err)
	require.NotEmpty(t, rev3)
}

func TestClient_BulkDocsAndRevsDiff(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	results, err := db.BulkDocs(ctx, []*model.Document{
		{ID: "d1", Data: map[string]interface{}{"n": 1}},
		{ID: "d2", Data: map[string]interface{}{"n": 2}},
	}, true)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, r := range results {
		require.True(t, r.Ok, "doc %s should be written", r.ID)
	}

	diff, err := db.RevsDiff(ctx, map[string][]string{"d1": {"1-doesnotexist"}})
	require.NoError(t, err)
	require.Contains(t, diff, "d1")
	require.Contains(t, diff["d1"].Missing, "1-doesnotexist")
}

func TestClient_AllDocs(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	_, err := db.BulkDocs(ctx, []*model.Document{
		{ID: "a", Data: map[string]interface{}{}},
		{ID: "b", Data: map[string]interface{}{}},
	}, true)
	require.NoError(t, err)

	resp, err := db.AllDocs(ctx, client.AllDocsQuery{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(resp.Rows), 2)
}

func TestClient_Changes(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	_, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	resp, err := db.Changes(ctx, &model.ChangesOptions{})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "doc1", resp.Results[0].ID)
}

func TestClient_ChangesContinuous(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	received := make(chan model.ChangeResult, 1)
	go func() {
		_ = db.ChangesContinuous(streamCtx, &model.ChangesOptions{Since: "now"}, func(c model.ChangeResult) error {
			select {
			case received <- c:
			default:
			}
			return nil
		})
	}()

	time.Sleep(100 * time.Millisecond)
	_, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	select {
	case change := <-received:
		require.Equal(t, "doc1", change.ID)
	case <-time.After(4 * time.Second):
		t.Fatal("did not receive continuous change in time")
	}
}

func TestClient_ChangesEventSource(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	received := make(chan model.ChangeResult, 1)
	go func() {
		_ = db.ChangesEventSource(streamCtx, &model.ChangesOptions{Since: "now"}, func(c model.ChangeResult) error {
			select {
			case received <- c:
			default:
			}
			return nil
		})
	}()

	time.Sleep(100 * time.Millisecond)
	_, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	select {
	case change := <-received:
		require.Equal(t, "doc1", change.ID)
	case <-time.After(4 * time.Second):
		t.Fatal("did not receive eventsource change in time")
	}
}

func TestClient_Raw(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	resp, err := db.Raw(ctx, http.MethodPut, "/doc1", strings.NewReader("not json"), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body["error"])
}

func TestClient_PutMultipart(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	rev, err := db.PutMultipart(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"hello": "world"}}, []client.MultipartAttachment{
		{Filename: "note.txt", ContentType: "text/plain", Data: []byte("hello multipart")},
	})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	att, err := db.GetAttachment(ctx, "doc1", "note.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(att)
	require.NoError(t, err)
	require.NoError(t, att.Close())
	require.Equal(t, "hello multipart", string(data))
}

func TestClient_Find(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	_, err := db.BulkDocs(ctx, []*model.Document{
		{ID: "a", Data: map[string]interface{}{"type": "fruit", "name": "apple"}},
		{ID: "b", Data: map[string]interface{}{"type": "veg", "name": "carrot"}},
	}, true)
	require.NoError(t, err)

	resp, err := db.Find(ctx, client.FindQuery{
		Selector: map[string]interface{}{"type": "fruit"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Docs, 1)
	require.Equal(t, "apple", resp.Docs[0]["name"])
}

func TestClient_View(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	_, err := db.BulkDocs(ctx, []*model.Document{
		{ID: "alice", Data: map[string]interface{}{"name": "alice", "type": "user"}},
		{ID: "bob", Data: map[string]interface{}{"name": "bob", "type": "user"}},
	}, true)
	require.NoError(t, err)

	_, err = db.Put(ctx, &model.Document{
		ID: "_design/users",
		Data: map[string]interface{}{
			"views": map[string]interface{}{
				"by_name": map[string]interface{}{
					"map": `function(doc) { if (doc.type === "user") { emit(doc.name, null); } }`,
				},
			},
		},
	})
	require.NoError(t, err)

	resp, err := db.View(ctx, "users", "by_name", client.ViewOptions{})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 2)
}

func TestClient_Security(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	// Include "admin" among the security admins -- Authenticator.DB has no
	// automatic server-admin bypass, so a security doc that didn't name
	// "admin" would lock the admin/secret session itself out of the
	// following (admin-only) GetSecurity call.
	sec := &model.Security{
		Admins:  model.Admins{Names: []string{"admin"}},
		Members: model.Members{Names: []string{"bob"}},
	}
	require.NoError(t, db.PutSecurity(ctx, sec))

	got, err := db.GetSecurity(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"admin"}, got.Admins.Names)
	require.Equal(t, []string{"bob"}, got.Members.Names)
}

func TestClient_RevsLimit(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	require.NoError(t, db.SetRevsLimit(ctx, 500))
	limit, err := db.GetRevsLimit(ctx)
	require.NoError(t, err)
	require.Equal(t, 500, limit)
}

func TestClient_OverUnixSocket(t *testing.T) {
	c := setupUnixTestServer(t)
	ctx := context.Background()

	require.NoError(t, c.Up(ctx))

	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))
	rev, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"ok": true}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)
}

// setupSocketPairServer is setupTestServer but wired to the client via a
// Unix socketpair instead of any listening socket -- no file, no port.
func setupSocketPairServer(t *testing.T) *client.Client {
	t.Helper()

	log := logger.NewNoLog()
	stor, err := storage.Open(t.TempDir(), storage.WithLogger(log))
	require.NoError(t, err)
	require.NoError(t, stor.EnsureSystemDatabases(context.Background()))

	r := mux.NewRouter()
	err = handler.Router{
		Storage:      stor,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: stor, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	c, closeServer, err := client.NewPaired(r, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeServer() })
	t.Cleanup(func() { _ = stor.Close() })

	return c
}

func TestClient_OverSocketPair(t *testing.T) {
	c := setupSocketPairServer(t)
	ctx := context.Background()

	require.NoError(t, c.Up(ctx))

	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))
	rev, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"ok": true}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	doc, err := db.Get(ctx, "doc1")
	require.NoError(t, err)
	require.Equal(t, true, doc.Data["ok"])
}

func TestClient_NewPaired_PanicsOnConflictingWithHTTPClient(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	assert.Panics(t, func() {
		_, _, _ = client.NewPaired(handler, client.WithHTTPClient(&http.Client{}))
	}, "NewPaired must panic when a caller's WithHTTPClient would clobber the paired transport")
}

func TestClient_Session(t *testing.T) {
	c := setupTestServer(t)
	ctx := context.Background()

	info, err := c.CreateSession(ctx, "admin", "secret")
	require.NoError(t, err)
	require.True(t, info.Ok)
	require.Equal(t, "admin", info.Name)

	require.NoError(t, c.DeleteSession(ctx))
}
