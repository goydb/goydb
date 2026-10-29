//go:build !nogoja

package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupActiveTasksTest creates a storage + client. Note: setupClientTest
// always runs the background controller.Task goroutine, so a task queued by
// a test must be observed quickly (its 500ms ticker interval) before it gets
// processed and removed from the queue.
func setupActiveTasksTest(t *testing.T) (*storage.Storage, *client.Client, func()) {
	t.Helper()

	log := logger.NewNoLog()
	c, s, cleanup := setupClientTest(t,
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
		storage.WithReducerEngine("", gojaview.NewReducerBuilder(log)),
		storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(log)),
	)

	return s, c, cleanup
}

func getActiveTasks(t *testing.T, c *client.Client) ([]Task, int) {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodGet, "/_active_tasks", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	var tasks []Task
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&tasks))
	}
	return tasks, resp.StatusCode
}

func TestActiveTasks_ViewIndexType(t *testing.T) {
	s, c, cleanup := setupActiveTasksTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDesignDoc(t, c, "testdb", "myddoc", map[string]interface{}{
		"views": map[string]interface{}{
			"myview": map[string]interface{}{
				"map": "function(doc) { emit(doc._id, 1); }",
			},
		},
	})

	tasks, code := getActiveTasks(t, c)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, tasks, 1)
	assert.Equal(t, "indexer", tasks[0].Type)
	assert.Equal(t, "_design/myddoc", tasks[0].DesignDocument)
	assert.Equal(t, "testdb", tasks[0].Database)
}

func TestActiveTasks_UpdatedOn(t *testing.T) {
	s, c, cleanup := setupActiveTasksTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDesignDoc(t, c, "testdb", "myddoc", map[string]interface{}{
		"views": map[string]interface{}{
			"myview": map[string]interface{}{
				"map": "function(doc) { emit(doc._id, 1); }",
			},
		},
	})

	tasks, code := getActiveTasks(t, c)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, tasks, 1)
	// started_on should be set (non-zero unix timestamp)
	assert.NotZero(t, tasks[0].StartedOn)
	// updated_on should also be set
	assert.NotZero(t, tasks[0].UpdatedOn)
}

func TestActiveTasks_Empty(t *testing.T) {
	_, c, cleanup := setupActiveTasksTest(t)
	defer cleanup()

	tasks, code := getActiveTasks(t, c)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, tasks)
}
