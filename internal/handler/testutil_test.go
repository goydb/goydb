package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/require"
)

// setupClientTest spins up a real in-process goydb HTTP server (admin/secret)
// and returns a *client.Client against it, the underlying storage.Storage
// (for direct fixture seeding -- not for exercising the API under test, which
// should go through the returned client), and a cleanup func.
//
// storageOpts are passed through to storage.Open, e.g. storage.WithViewEngine
// / storage.WithReducerEngine for tests that query views. The background
// controller.Task goroutine (which builds view/mango indices) always runs,
// since it's needed by any test touching views and is a no-op otherwise.
func setupClientTest(t *testing.T, storageOpts ...storage.StorageOption) (*client.Client, *storage.Storage, func()) {
	t.Helper()

	log := logger.NewNoLog()
	opts := append([]storage.StorageOption{storage.WithLogger(log)}, storageOpts...)
	s, err := storage.Open(t.TempDir(), opts...)
	require.NoError(t, err)

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough")),
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	taskCtx, cancelTasks := context.WithCancel(context.Background())
	tc := controller.Task{Storage: s, Logger: log}
	go tc.Run(taskCtx)

	ts := httptest.NewServer(r)

	c, err := client.New(ts.URL, client.WithBasicAuth("admin", "secret"))
	require.NoError(t, err)

	return c, s, func() {
		cancelTasks()
		ts.Close()
		_ = s.Close()
	}
}
