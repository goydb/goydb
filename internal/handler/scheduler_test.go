package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type schedulerJobsResponse struct {
	TotalRows int            `json:"total_rows"`
	Offset    int            `json:"offset"`
	Jobs      []SchedulerJob `json:"jobs"`
}

func getSchedulerJobs(t *testing.T, router http.Handler) schedulerJobsResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/_scheduler/jobs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp schedulerJobsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	return resp
}

// TestSchedulerJobs_NoReplicatorDB verifies that when the _replicator database
// does not exist the endpoint returns an empty job list without error.
func TestSchedulerJobs_NoReplicatorDB(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp := getSchedulerJobs(t, router)
	assert.Equal(t, 0, resp.TotalRows)
	assert.Equal(t, 0, resp.Offset)
	assert.Empty(t, resp.Jobs)
}

// TestSchedulerJobs_EmptyReplicatorDB verifies that when _replicator exists but
// contains no valid replication docs the response is still empty.
func TestSchedulerJobs_EmptyReplicatorDB(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "_replicator")
	require.NoError(t, err)

	resp := getSchedulerJobs(t, router)
	assert.Equal(t, 0, resp.TotalRows)
	assert.Empty(t, resp.Jobs)
}

// TestSchedulerJobs_SingleRunningJob verifies that a replication doc with state
// "running" is returned as a job with status "running".
func TestSchedulerJobs_SingleRunningJob(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID: "myjob",
		Data: map[string]interface{}{
			"source":                   "http://source/db",
			"target":                   "http://target/db",
			"_replication_state":       "running",
			"_replication_state_time":  "2024-06-01T12:00:00Z",
		},
	})
	require.NoError(t, err)

	resp := getSchedulerJobs(t, router)
	require.Equal(t, 1, resp.TotalRows)
	require.Len(t, resp.Jobs, 1)

	job := resp.Jobs[0]
	assert.Equal(t, "_replicator", job.Database)
	assert.Equal(t, "myjob", job.ID)
	assert.Equal(t, "myjob", job.DocID)
	assert.Equal(t, "http://source/db", job.Source)
	assert.Equal(t, "http://target/db", job.Target)
	assert.Equal(t, "running", job.Status)
	assert.Equal(t, "nonode@nohost", job.Node)
	assert.Nil(t, job.User)
	assert.NotEmpty(t, job.Pid)
	assert.Equal(t, "2024-06-01T12:00:00Z", job.StartTime)
}

// TestSchedulerJobs_StateMapping verifies that each _replication_state value
// maps to the correct scheduler status string.
func TestSchedulerJobs_StateMapping(t *testing.T) {
	cases := []struct {
		state          string
		expectedStatus string
	}{
		{"", "added"},
		{"initializing", "added"},
		{"running", "running"},
		{"completed", "completed"},
		{"error", "crashing"},
		{"crashing", "crashing"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.state, func(t *testing.T) {
			s, router, cleanup := setupRevsDiffTest(t)
			defer cleanup()

			ctx := t.Context()
			db, err := s.CreateDatabase(ctx, "_replicator")
			require.NoError(t, err)

			data := map[string]interface{}{
				"source": "http://source/db",
				"target": "http://target/db",
			}
			if tc.state != "" {
				data["_replication_state"] = tc.state
			}
			_, err = db.PutDocument(ctx, &model.Document{ID: "job1", Data: data})
			require.NoError(t, err)

			resp := getSchedulerJobs(t, router)
			require.Len(t, resp.Jobs, 1)
			assert.Equal(t, tc.expectedStatus, resp.Jobs[0].Status)
		})
	}
}

// TestSchedulerJobs_DesignDocsSkipped verifies that design documents in
// _replicator are not included in the jobs list.
func TestSchedulerJobs_DesignDocsSkipped(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	// Design doc — should be ignored
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "_design/myfilter",
		Data: map[string]interface{}{"filters": map[string]interface{}{}},
	})
	require.NoError(t, err)

	// Valid replication doc
	_, err = db.PutDocument(ctx, &model.Document{
		ID: "realjob",
		Data: map[string]interface{}{
			"source": "http://a/db",
			"target": "http://b/db",
		},
	})
	require.NoError(t, err)

	resp := getSchedulerJobs(t, router)
	require.Equal(t, 1, resp.TotalRows)
	require.Len(t, resp.Jobs, 1)
	assert.Equal(t, "realjob", resp.Jobs[0].ID)
}

// TestSchedulerJobs_InvalidDocSkipped verifies that docs missing source or
// target fields are silently skipped.
func TestSchedulerJobs_InvalidDocSkipped(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	// No source/target — should be skipped
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "baddoc",
		Data: map[string]interface{}{"foo": "bar"},
	})
	require.NoError(t, err)

	resp := getSchedulerJobs(t, router)
	assert.Equal(t, 0, resp.TotalRows)
	assert.Empty(t, resp.Jobs)
}

// TestSchedulerJobs_MultipleJobs verifies that multiple replication docs are
// all returned with correct fields.
func TestSchedulerJobs_MultipleJobs(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	jobs := []struct {
		id     string
		source string
		target string
		state  string
	}{
		{"job-a", "http://src/a", "http://tgt/a", "completed"},
		{"job-b", "http://src/b", "http://tgt/b", "running"},
		{"job-c", "http://src/c", "http://tgt/c", "error"},
	}

	for _, j := range jobs {
		_, err = db.PutDocument(ctx, &model.Document{
			ID: j.id,
			Data: map[string]interface{}{
				"source":             j.source,
				"target":             j.target,
				"_replication_state": j.state,
			},
		})
		require.NoError(t, err)
	}

	resp := getSchedulerJobs(t, router)
	assert.Equal(t, 3, resp.TotalRows)
	assert.Len(t, resp.Jobs, 3)

	byID := make(map[string]SchedulerJob, len(resp.Jobs))
	for _, j := range resp.Jobs {
		byID[j.ID] = j
	}

	assert.Equal(t, "completed", byID["job-a"].Status)
	assert.Equal(t, "http://src/a", byID["job-a"].Source)
	assert.Equal(t, "http://tgt/a", byID["job-a"].Target)

	assert.Equal(t, "running", byID["job-b"].Status)
	assert.Equal(t, "crashing", byID["job-c"].Status)
}

// TestSchedulerJobs_HistoryAndInfo verifies that jobs include history entries
// and info fields.
func TestSchedulerJobs_HistoryAndInfo(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID: "myjob",
		Data: map[string]interface{}{
			"source":                          "http://source/db",
			"target":                          "http://target/db",
			"_replication_state":              "error",
			"_replication_state_reason":       "db_not_found",
			"_replication_state_time":         "2024-06-01T12:00:00Z",
			"_replication_consecutive_fails":  float64(3),
		},
	})
	require.NoError(t, err)

	resp := getSchedulerJobs(t, router)
	require.Len(t, resp.Jobs, 1)

	job := resp.Jobs[0]
	assert.Equal(t, "crashing", job.Status)
	assert.Equal(t, 3, job.ErrorCount)
	assert.NotEmpty(t, job.LastUpdated)
	require.Len(t, job.History, 1)
	assert.Equal(t, "crashing", job.History[0]["type"])
	assert.Equal(t, "db_not_found", job.Info["error"])
}

// --- Scheduler Docs Tests ---

type schedulerDocsResponse struct {
	TotalRows int                `json:"total_rows"`
	Offset    int                `json:"offset"`
	Docs      []SchedulerDocInfo `json:"docs"`
}

func getSchedulerDocs(t *testing.T, router http.Handler) schedulerDocsResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/_scheduler/docs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp schedulerDocsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	return resp
}

// TestSchedulerDocs_NoReplicatorDB verifies empty response when _replicator doesn't exist.
func TestSchedulerDocs_NoReplicatorDB(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	resp := getSchedulerDocs(t, router)
	assert.Equal(t, 0, resp.TotalRows)
	assert.Empty(t, resp.Docs)
}

// TestSchedulerDocs_ListsDocs verifies docs are returned from _replicator.
func TestSchedulerDocs_ListsDocs(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID: "rep1",
		Data: map[string]interface{}{
			"source":             "http://src/a",
			"target":             "http://tgt/a",
			"_replication_state": "running",
		},
	})
	require.NoError(t, err)

	resp := getSchedulerDocs(t, router)
	require.Equal(t, 1, resp.TotalRows)
	require.Len(t, resp.Docs, 1)

	doc := resp.Docs[0]
	assert.Equal(t, "_replicator", doc.Database)
	assert.Equal(t, "rep1", doc.DocID)
	assert.Equal(t, "http://src/a", doc.Source)
	assert.Equal(t, "http://tgt/a", doc.Target)
	assert.Equal(t, "running", doc.State)
	assert.Equal(t, "nonode@nohost", doc.Node)
}

// TestSchedulerDocByID_Found verifies individual doc lookup.
func TestSchedulerDocByID_Found(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "_replicator")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID: "rep-abc",
		Data: map[string]interface{}{
			"source":             "http://src/x",
			"target":             "http://tgt/x",
			"_replication_state": "completed",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/_scheduler/docs/rep-abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var doc SchedulerDocInfo
	require.NoError(t, json.NewDecoder(w.Body).Decode(&doc))
	assert.Equal(t, "rep-abc", doc.DocID)
	assert.Equal(t, "http://src/x", doc.Source)
	assert.Equal(t, "http://tgt/x", doc.Target)
	assert.Equal(t, "completed", doc.State)
}

// TestSchedulerDocByID_NotFound verifies 404 for missing doc.
func TestSchedulerDocByID_NotFound(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "_replicator")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/_scheduler/docs/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
