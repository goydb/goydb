//go:build !nogoja

package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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

func setupChangesTest(t *testing.T) (*storage.Storage, *mux.Router, func()) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-changes-test-*")
	require.NoError(t, err)

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
	require.NoError(t, err)

	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	r := mux.NewRouter()
	err = Router{
		Storage:      s,
		SessionStore: store,
		Admins:       model.AdminUsers{model.AdminUser{Username: "admin", Password: "secret"}},
		Replication:  &service.Replication{Storage: s, Logger: logger.NewNoLog()},
		Logger:       logger.NewNoLog(),
	}.Build(r)
	require.NoError(t, err)

	return s, r, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

type ChangesResponse struct {
	Results []ChangeDoc `json:"results"`
	LastSeq string      `json:"last_seq"`
	Pending int         `json:"pending"`
}

func TestDBChanges_FeedNormal(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create some documents
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Get changes with normal feed
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=normal", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	assert.Len(t, result.Results, 2)
	assert.Equal(t, "doc1", result.Results[0].ID)
	assert.Equal(t, "doc2", result.Results[1].ID)
	assert.NotEmpty(t, result.LastSeq)
}

func TestDBChanges_FeedNormal_IncludeDocs(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 42},
	})
	require.NoError(t, err)

	// Get changes with include_docs
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=normal&include_docs=true", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	require.Len(t, result.Results, 1)
	require.NotNil(t, result.Results[0].Doc)
	assert.Equal(t, float64(42), result.Results[0].Doc["value"])
}

func TestDBChanges_FeedLongpoll(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create initial document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Start longpoll request with since=now (waits for new changes)
	responseChan := make(chan *httptest.ResponseRecorder)
	go func() {
		req := httptest.NewRequest("GET", "/testdb/_changes?feed=longpoll&since=now&timeout=5000", nil)
		req.SetBasicAuth("admin", "secret")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		responseChan <- w
	}()

	// Wait a bit to ensure longpoll is waiting
	time.Sleep(100 * time.Millisecond)

	// Add a new document while longpoll is waiting
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Should receive response relatively quickly
	select {
	case w := <-responseChan:
		require.Equal(t, http.StatusOK, w.Code)

		var result ChangesResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

		// Should receive the new document
		assert.Len(t, result.Results, 1)
		assert.Equal(t, "doc2", result.Results[0].ID)

	case <-time.After(2 * time.Second):
		t.Fatal("longpoll did not return after document was created")
	}
}

func TestDBChanges_FeedContinuous(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create initial document.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Start continuous feed WITHOUT include_docs.
	pr, pw := NewPipeRecorder()
	defer func() { _ = pw.Close() }()

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=continuous&timeout=3000", nil)
	req = req.WithContext(reqCtx)
	req.SetBasicAuth("admin", "secret")

	go func() {
		router.ServeHTTP(pw, req)
	}()

	scanner := bufio.NewScanner(pr)

	// Read initial change (doc1).
	require.True(t, scanner.Scan(), "should receive initial change")
	var initial ChangeDoc
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &initial))
	assert.Equal(t, "doc1", initial.ID)
	assert.Nil(t, initial.Doc, "doc should be nil without include_docs")
	assert.NotEqual(t, "0", initial.Seq, "seq should not be 0")

	// Add a new document while the feed is open.
	time.Sleep(50 * time.Millisecond)
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Read the live change.
	liveDone := make(chan ChangeDoc, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var change ChangeDoc
			if err := json.Unmarshal([]byte(line), &change); err == nil && change.ID == "doc2" {
				liveDone <- change
				return
			}
		}
	}()

	select {
	case change := <-liveDone:
		assert.Equal(t, "doc2", change.ID)
		assert.Nil(t, change.Doc, "doc should be nil without include_docs")
		assert.NotEqual(t, "0", change.Seq, "live change seq should not be 0")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for live change — live changes may be dropped")
	}

	cancel()
}

// TestDBChanges_FeedContinuous_IncludeDocs verifies that live changes in
// the continuous feed include the document body when include_docs=true
// and that the seq field is a real sequence number (not "0").
func TestDBChanges_FeedContinuous_IncludeDocs(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create initial document so the feed has something to start with.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Start continuous feed with include_docs=true.
	pr, pw := NewPipeRecorder()
	defer func() { _ = pw.Close() }()

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=continuous&include_docs=true&timeout=3000", nil)
	req = req.WithContext(reqCtx)
	req.SetBasicAuth("admin", "secret")

	go func() {
		router.ServeHTTP(pw, req)
	}()

	scanner := bufio.NewScanner(pr)

	// Read initial change (doc1).
	require.True(t, scanner.Scan(), "should receive initial change")
	var initial ChangeDoc
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &initial))
	assert.Equal(t, "doc1", initial.ID)
	assert.NotNil(t, initial.Doc, "initial change should include doc body")
	assert.NotEqual(t, "0", initial.Seq, "seq should not be 0")

	// Now add a new document while the feed is open.
	time.Sleep(50 * time.Millisecond) // let the feed handler settle
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Read the live change.
	liveDone := make(chan ChangeDoc, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var change ChangeDoc
			if err := json.Unmarshal([]byte(line), &change); err == nil && change.ID == "doc2" {
				liveDone <- change
				return
			}
		}
	}()

	select {
	case change := <-liveDone:
		assert.Equal(t, "doc2", change.ID)
		require.NotNil(t, change.Doc, "live change should include doc body with include_docs=true")
		assert.Equal(t, float64(2), change.Doc["value"], "doc body should contain the document data")
		assert.Equal(t, "doc2", change.Doc["_id"])
		assert.NotEmpty(t, change.Doc["_rev"])
		assert.NotEqual(t, "0", change.Seq, "live change seq should not be 0")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for live change — live changes may be dropped")
	}

	cancel()
}

func TestDBChanges_Heartbeat(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a document first
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Test that heartbeat parameter is accepted and doesn't break normal operation
	// Note: httptest.ResponseRecorder doesn't implement http.Flusher, so actual
	// heartbeat sending won't happen in this test. This just verifies the parameter
	// is parsed and doesn't cause errors.

	req := httptest.NewRequest("GET", "/testdb/_changes?feed=normal&heartbeat=1000", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should receive the document
	require.Len(t, result.Results, 1)
	assert.Equal(t, "doc1", result.Results[0].ID)
}

func TestDBChanges_Timeout(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Start longpoll with short timeout and no new changes
	start := time.Now()
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=longpoll&since=now&timeout=1000", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	elapsed := time.Since(start)

	require.Equal(t, http.StatusOK, w.Code)

	// Should have waited approximately 1 second (timeout)
	// Allow some margin for test execution time
	assert.Greater(t, elapsed, 800*time.Millisecond, "should wait at least 800ms")
	assert.Less(t, elapsed, 3*time.Second, "should not wait more than 3s")

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should return empty results after timeout
	assert.Empty(t, result.Results)
}

func TestDBChanges_DocIDsFilter(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create multiple documents
	for i := 1; i <= 5; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	// Filter for only doc2 and doc4 using POST with doc_ids
	bodyData := map[string]interface{}{
		"doc_ids": []string{"doc2", "doc4"},
	}
	body, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/testdb/_changes?feed=normal", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive doc2 and doc4
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "doc2")
	assert.Contains(t, docIDs, "doc4")
}

func TestDBChanges_Since(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create first document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Get current sequence
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=normal", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var result1 ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result1))
	firstSeq := result1.LastSeq

	// Create second document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Get changes since first sequence
	req = httptest.NewRequest("GET", fmt.Sprintf("/testdb/_changes?feed=normal&since=%s", firstSeq), nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var result2 ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result2))

	// Should only receive doc2
	require.Len(t, result2.Results, 1)
	assert.Equal(t, "doc2", result2.Results[0].ID)
}

func TestDBChanges_Deleted(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create and then delete a document
	rev, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	_, err = db.DeleteDocument(ctx, "doc1", rev)
	require.NoError(t, err)

	// Get changes
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=normal", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should receive both create and delete changes
	require.GreaterOrEqual(t, len(result.Results), 1)

	// Find the latest change for doc1
	var doc1Change *ChangeDoc
	for i := range result.Results {
		if result.Results[i].ID == "doc1" {
			doc1Change = &result.Results[i]
		}
	}

	require.NotNil(t, doc1Change)
	assert.True(t, doc1Change.Deleted, "document should be marked as deleted")
}

// PipeRecorder is a custom ResponseRecorder that supports streaming
type PipeRecorder struct {
	*httptest.ResponseRecorder
	pipe       *io.PipeReader
	pipeWriter *io.PipeWriter
}

func NewPipeRecorder() (*io.PipeReader, *PipeRecorder) {
	pr, pw := io.Pipe()
	rec := &PipeRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		pipe:             pr,
		pipeWriter:       pw,
	}
	return pr, rec
}

func (pr *PipeRecorder) Write(buf []byte) (int, error) {
	// Write to both the underlying recorder and the pipe
	_, _ = pr.ResponseRecorder.Write(buf)
	return pr.pipeWriter.Write(buf)
}

func (pr *PipeRecorder) Flush() {
	// Implement Flusher interface - no-op for pipe recorder
}

func (pr *PipeRecorder) Close() error {
	return pr.pipeWriter.Close()
}

func TestDBChanges_FeedEventSource(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Get changes with eventsource feed
	req := httptest.NewRequest("GET", "/testdb/_changes?feed=eventsource", nil)
	req.SetBasicAuth("admin", "secret")

	// Use pipe recorder for streaming
	pr, pw := NewPipeRecorder()
	defer func() { _ = pw.Close() }()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	// Start the request
	go func() {
		router.ServeHTTP(pw, req)
	}()

	// Read SSE formatted output
	scanner := bufio.NewScanner(pr)
	var receivedData bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			receivedData = true
			jsonData := strings.TrimPrefix(line, "data: ")
			var change ChangeDoc
			err := json.Unmarshal([]byte(jsonData), &change)
			require.NoError(t, err)
			assert.Equal(t, "doc1", change.ID)
			break
		}
	}

	assert.True(t, receivedData, "should receive SSE formatted data")
}

func TestDBChanges_FilterSelector(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create documents with different types
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "post1",
		Data: map[string]interface{}{"type": "post", "title": "Hello"},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "comment1",
		Data: map[string]interface{}{"type": "comment", "text": "Great!"},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "post2",
		Data: map[string]interface{}{"type": "post", "title": "World"},
	})
	require.NoError(t, err)

	// Filter by type=post using selector
	bodyData := map[string]interface{}{
		"selector": map[string]interface{}{
			"type": "post",
		},
	}
	body, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/testdb/_changes?filter=_selector", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive posts
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "post1")
	assert.Contains(t, docIDs, "post2")
	assert.NotContains(t, docIDs, "comment1")
}

func TestDBChanges_FilterView(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create design doc with view
	_, err = db.PutDocument(ctx, &model.Document{
		ID: "_design/blog",
		Data: map[string]interface{}{
			"views": map[string]interface{}{
				"published": map[string]interface{}{
					"map": "function(doc) { if (doc.published) emit(doc._id, 1); }",
				},
			},
		},
	})
	require.NoError(t, err)

	// Create published and unpublished docs
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "post1",
		Data: map[string]interface{}{"title": "A", "published": true},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "draft1",
		Data: map[string]interface{}{"title": "B", "published": false},
	})
	require.NoError(t, err)

	// Filter using view (CouchDB format: no _design/ prefix)
	req := httptest.NewRequest("GET", "/testdb/_changes?filter=_view&view=blog/published", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive published posts (not design doc, not draft)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "post1", result.Results[0].ID)
}

func TestDBChanges_FilterCustom(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create design doc with filter function
	_, err = db.PutDocument(ctx, &model.Document{
		ID: "_design/app",
		Data: map[string]interface{}{
			"filters": map[string]interface{}{
				"important": "function(doc, req) { return doc.priority >= parseInt(req.query.minPriority || '0'); }",
			},
		},
	})
	require.NoError(t, err)

	// Create documents with priorities
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "task1",
		Data: map[string]interface{}{"task": "Fix bug", "priority": 5},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "task2",
		Data: map[string]interface{}{"task": "Nice to have", "priority": 1},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "task3",
		Data: map[string]interface{}{"task": "Critical", "priority": 10},
	})
	require.NoError(t, err)

	// Filter by priority >= 3 (CouchDB format: no _design/ prefix)
	req := httptest.NewRequest("GET", "/testdb/_changes?filter=app/important&minPriority=3", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive task1 and task3 (priority >= 3)
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "task1")
	assert.Contains(t, docIDs, "task3")
	assert.NotContains(t, docIDs, "task2")
}

func TestDBChanges_FilterCombinations(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create multiple documents
	for i := 1; i <= 5; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID: fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{
				"type":  "post",
				"value": i,
			},
		})
		require.NoError(t, err)
	}

	// Test combining doc_ids with selector filter
	bodyData := map[string]interface{}{
		"doc_ids": []string{"doc2", "doc3", "doc4"},
		"selector": map[string]interface{}{
			"value": map[string]interface{}{
				"$gte": 3, // value >= 3
			},
		},
	}
	body, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/testdb/_changes?filter=_selector", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive doc3 and doc4 (in doc_ids AND value >= 3)
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "doc3")
	assert.Contains(t, docIDs, "doc4")
	assert.NotContains(t, docIDs, "doc2") // filtered out by selector
}

func TestDBChanges_EventSourceWithFilter(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create documents with different types
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "post1",
		Data: map[string]interface{}{"type": "post"},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "comment1",
		Data: map[string]interface{}{"type": "comment"},
	})
	require.NoError(t, err)

	// Test EventSource with selector filter
	bodyData := map[string]interface{}{
		"selector": map[string]interface{}{
			"type": "post",
		},
	}
	body, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/testdb/_changes?feed=eventsource&filter=_selector", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")

	pr, pw := NewPipeRecorder()
	defer func() { _ = pw.Close() }()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	go func() {
		router.ServeHTTP(pw, req)
	}()

	// Read SSE formatted output
	scanner := bufio.NewScanner(pr)
	var receivedChanges []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var change ChangeDoc
			if err := json.Unmarshal([]byte(jsonData), &change); err == nil {
				receivedChanges = append(receivedChanges, change.ID)
			}
			if len(receivedChanges) >= 1 {
				break
			}
		}
	}

	// Should only receive post1
	require.Len(t, receivedChanges, 1)
	assert.Equal(t, "post1", receivedChanges[0])
}

func TestDBChanges_FilterCustom_DesignPrefix(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create design doc with filter function
	_, err = db.PutDocument(ctx, &model.Document{
		ID: "_design/app",
		Data: map[string]interface{}{
			"filters": map[string]interface{}{
				"important": "function(doc, req) { return doc.priority >= parseInt(req.query.minPriority || '0'); }",
			},
		},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "task1",
		Data: map[string]interface{}{"task": "Fix bug", "priority": 5},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "task2",
		Data: map[string]interface{}{"task": "Nice to have", "priority": 1},
	})
	require.NoError(t, err)

	// Use _design/ prefix format (backwards compatibility)
	req := httptest.NewRequest("GET", "/testdb/_changes?filter=_design/app/important&minPriority=3", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive task1 (priority >= 3)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "task1", result.Results[0].ID)
}

func TestDBChanges_FilterDesign(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a design document
	_, err = db.PutDocument(ctx, &model.Document{
		ID: "_design/myapp",
		Data: map[string]interface{}{
			"views": map[string]interface{}{
				"all": map[string]interface{}{
					"map": "function(doc) { emit(doc._id, 1); }",
				},
			},
		},
	})
	require.NoError(t, err)

	// Create regular documents
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Filter to only design documents
	req := httptest.NewRequest("GET", "/testdb/_changes?filter=_design", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive the design document
	require.Len(t, result.Results, 1)
	assert.Equal(t, "_design/myapp", result.Results[0].ID)
}

func TestDBChanges_FilterView_DesignPrefix(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create design doc with view
	_, err = db.PutDocument(ctx, &model.Document{
		ID: "_design/blog",
		Data: map[string]interface{}{
			"views": map[string]interface{}{
				"published": map[string]interface{}{
					"map": "function(doc) { if (doc.published) emit(doc._id, 1); }",
				},
			},
		},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "post1",
		Data: map[string]interface{}{"title": "A", "published": true},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "draft1",
		Data: map[string]interface{}{"title": "B", "published": false},
	})
	require.NoError(t, err)

	// Use _design/ prefix format (backwards compatibility)
	req := httptest.NewRequest("GET", "/testdb/_changes?filter=_view&view=_design/blog/published", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive published posts
	require.Len(t, result.Results, 1)
	assert.Equal(t, "post1", result.Results[0].ID)
}

func TestDBChanges_FilterCustom_UserCtx(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create design doc with filter that checks userCtx
	_, err = db.PutDocument(ctx, &model.Document{
		ID: "_design/app",
		Data: map[string]interface{}{
			"filters": map[string]interface{}{
				"by_user": "function(doc, req) { return doc.author === req.userCtx.name; }",
			},
		},
	})
	require.NoError(t, err)

	// Create documents by different authors
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"author": "admin", "text": "Hello"},
	})
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"author": "other", "text": "World"},
	})
	require.NoError(t, err)

	// Filter using custom filter that checks userCtx.name (authenticated as "admin")
	req := httptest.NewRequest("GET", "/testdb/_changes?filter=app/by_user", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	// Should only receive doc1 (authored by "admin")
	require.Len(t, result.Results, 1)
	assert.Equal(t, "doc1", result.Results[0].ID)
}

func TestDBChanges_FilterDocIDs_ExplicitFilter(t *testing.T) {
	s, router, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		_, err = db.PutDocument(ctx, &model.Document{
			ID:   fmt.Sprintf("doc%d", i),
			Data: map[string]interface{}{"value": i},
		})
		require.NoError(t, err)
	}

	// Use filter=_doc_ids with doc_ids in POST body
	bodyData := map[string]interface{}{
		"doc_ids": []string{"doc1", "doc3"},
	}
	body, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/testdb/_changes?filter=_doc_ids", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChangesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))

	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "doc1")
	assert.Contains(t, docIDs, "doc3")
	assert.NotContains(t, docIDs, "doc2")
}
