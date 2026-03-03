//go:build !nogoja

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupActiveTasksTest creates a storage + router without starting the task
// controller, so tasks remain in the queue and are visible in /_active_tasks.
func setupActiveTasksTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	t.Helper()

	dir, err := os.MkdirTemp(os.TempDir(), "goydb-active-tasks-test-*")
	require.NoError(t, err)

	log := logger.NewNoLog()
	s, err := storage.Open(dir,
		storage.WithLogger(log),
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
		storage.WithReducerEngine("", gojaview.NewReducerBuilder(log)),
		storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(log)),
	)
	require.NoError(t, err)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: log},
		Logger:       log,
	}.Build(r)
	require.NoError(t, err)

	return s, r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func getActiveTasks(t *testing.T, router *mux.Router) ([]Task, int) {
	t.Helper()
	req := httptest.NewRequest("GET", "/_active_tasks", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var tasks []Task
	if w.Code == http.StatusOK {
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tasks))
	}
	return tasks, w.Code
}

func TestActiveTasks_ViewIndexType(t *testing.T) {
	s, router, cleanup := setupActiveTasksTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "myddoc", map[string]interface{}{
		"views": map[string]interface{}{
			"myview": map[string]interface{}{
				"map": "function(doc) { emit(doc._id, 1); }",
			},
		},
	})

	tasks, code := getActiveTasks(t, router)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, tasks, 1)
	assert.Equal(t, "indexer", tasks[0].Type)
	assert.Equal(t, "_design/myddoc", tasks[0].DesignDocument)
	assert.Equal(t, "testdb", tasks[0].Database)
}

func TestActiveTasks_UpdatedOn(t *testing.T) {
	s, router, cleanup := setupActiveTasksTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "myddoc", map[string]interface{}{
		"views": map[string]interface{}{
			"myview": map[string]interface{}{
				"map": "function(doc) { emit(doc._id, 1); }",
			},
		},
	})

	tasks, code := getActiveTasks(t, router)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, tasks, 1)
	// started_on should be set (non-zero unix timestamp)
	assert.NotZero(t, tasks[0].StartedOn)
	// updated_on should also be set
	assert.NotZero(t, tasks[0].UpdatedOn)
}

func TestActiveTasks_Empty(t *testing.T) {
	_, router, cleanup := setupActiveTasksTest(t)
	defer cleanup()

	tasks, code := getActiveTasks(t, router)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, tasks)
}
