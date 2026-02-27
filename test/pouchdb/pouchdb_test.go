package pouchdb_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
)

func TestPouchDBCompat(t *testing.T) {
	// Skip if Node.js or npm is not available.
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH, skipping PouchDB tests")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not found in PATH, skipping PouchDB tests")
	}

	// ── Start goydb ──────────────────────────────────────────────────
	dir := t.TempDir()

	log := logger.NewNoLog()
	s, err := storage.Open(
		dir,
		storage.WithLogger(log),
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
		storage.WithFilterEngine("", gojaview.NewFilterServer),
		storage.WithFilterEngine("javascript", gojaview.NewFilterServer),
		storage.WithReducerEngine("", gojaview.NewReducerBuilder(log)),
		storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(log)),
	)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	if err := s.EnsureSystemDatabases(t.Context()); err != nil {
		t.Fatalf("failed to ensure system databases: %v", err)
	}

	// Start the background task runner (processes view builds, etc.).
	taskCtx, taskCancel := context.WithCancel(t.Context())
	defer taskCancel()
	go controller.Task{Storage: s, Logger: log}.Run(taskCtx)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))
	r := mux.NewRouter()
	err = handler.Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	if err != nil {
		t.Fatalf("failed to build router: %v", err)
	}

	ts := httptest.NewServer(r)
	defer ts.Close()

	t.Logf("goydb listening on %s", ts.URL)

	// ── Install npm dependencies if needed ───────────────────────────
	testDir := filepath.Dir(mustAbs(t, "pouchdb_test.js"))
	nodeModules := filepath.Join(testDir, "node_modules")

	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		t.Log("Running npm install...")
		install := exec.Command("npm", "install", "--no-audit", "--no-fund")
		install.Dir = testDir
		install.Stdout = os.Stdout
		install.Stderr = os.Stderr
		if err := install.Run(); err != nil {
			t.Fatalf("npm install failed: %v", err)
		}
	}

	// ── Run PouchDB tests ────────────────────────────────────────────
	t.Log("Running PouchDB tests...")
	goydbURL := "http://" + ts.Listener.Addr().String()

	cmd := exec.Command("npm", "test")
	cmd.Dir = testDir
	cmd.Env = append(os.Environ(),
		"GOYDB_URL="+goydbURL,
		"GOYDB_USER=admin",
		"GOYDB_PASS=secret",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("PouchDB tests failed: %v", err)
	}
}

// mustAbs returns the absolute path of a file relative to the current file.
func mustAbs(t *testing.T, name string) string {
	t.Helper()
	// The test binary runs with the working directory set to the package
	// directory, so "pouchdb_test.js" is right next to us.
	abs, err := filepath.Abs(name)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", name, err)
	}
	return abs
}
