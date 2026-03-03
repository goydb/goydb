//go:build !nosearch && !nogoja

package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveTasks_SearchIndexType(t *testing.T) {
	s, router, cleanup := setupActiveTasksTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	putDesignDoc(t, router, "testdb", "myidx", map[string]interface{}{
		"indexes": map[string]interface{}{
			"search": map[string]interface{}{
				"index": `function(doc) {
					index("name", doc.name, {"store": true});
				}`,
			},
		},
	})

	tasks, code := getActiveTasks(t, router)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, tasks, 1)
	assert.Equal(t, "search_indexer", tasks[0].Type)
	assert.Equal(t, "_design/myidx", tasks[0].DesignDocument)
	assert.Equal(t, "testdb", tasks[0].Database)
}
