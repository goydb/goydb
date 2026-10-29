//go:build !nogoja

package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChangesTest(t *testing.T) (*storage.Storage, *client.Client, func()) {
	t.Helper()
	log := logger.NewNoLog()
	c, s, cleanup := setupClientTest(t,
		storage.WithViewEngine("", gojaview.NewViewServer),
		storage.WithViewEngine("javascript", gojaview.NewViewServer),
		storage.WithFilterEngine("", gojaview.NewFilterServer),
		storage.WithFilterEngine("javascript", gojaview.NewFilterServer),
		storage.WithReducerEngine("", gojaview.NewReducerBuilder(log)),
		storage.WithReducerEngine("javascript", gojaview.NewReducerBuilder(log)),
	)
	return s, c, cleanup
}

type ChangesResponse struct {
	Results []ChangeDoc `json:"results"`
	LastSeq string      `json:"last_seq"`
	Pending int         `json:"pending"`
}

// rawChangesGET issues a GET _changes request via Raw and decodes the full
// batch response, for request shapes (extra query params like "view" or
// "minPriority") that *client.Database's typed Changes doesn't model.
func rawChangesGET(t *testing.T, c *client.Client, dbName, query string) ChangesResponse {
	t.Helper()
	resp, err := c.Raw(t.Context(), http.MethodGet, "/"+dbName+"/_changes?"+query, nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result ChangesResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}

// rawChangesPOST is rawChangesGET for a POST body (doc_ids / selector
// filters), which the typed Changes method doesn't support either.
func rawChangesPOST(t *testing.T, c *client.Client, dbName, query string, body interface{}) ChangesResponse {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := c.Raw(t.Context(), http.MethodPost, "/"+dbName+"/_changes?"+query, bytes.NewReader(b), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result ChangesResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}

func TestDBChanges_FeedNormal(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal"})
	require.NoError(t, err)

	assert.Len(t, result.Results, 2)
	assert.Equal(t, "doc1", result.Results[0].ID)
	assert.Equal(t, "doc2", result.Results[1].ID)
	assert.NotEmpty(t, result.LastSeq)
}

func TestDBChanges_FeedNormal_IncludeDocs(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 42},
	})
	require.NoError(t, err)

	// include_docs isn't modeled by *client.Database's typed Changes (its
	// model.ChangeResult.Doc field is json:"-"), so fetch raw and decode
	// into the local ChangeDoc shape instead.
	result := rawChangesGET(t, c, "testdb", "feed=normal&include_docs=true")

	require.Len(t, result.Results, 1)
	require.NotNil(t, result.Results[0].Doc)
	assert.Equal(t, float64(42), result.Results[0].Doc["value"])
}

func TestDBChanges_FeedLongpoll(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create initial document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Start longpoll request with since=now (waits for new changes)
	type longpollResult struct {
		resp *model.ChangesResponse
		err  error
	}
	responseChan := make(chan longpollResult, 1)
	go func() {
		resp, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{
			Feed:    "longpoll",
			Since:   "now",
			Timeout: 5000 * time.Millisecond,
		})
		responseChan <- longpollResult{resp: resp, err: err}
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
	case r := <-responseChan:
		require.NoError(t, r.err)

		// Should receive the new document
		assert.Len(t, r.resp.Results, 1)
		assert.Equal(t, "doc2", r.resp.Results[0].ID)

	case <-time.After(2 * time.Second):
		t.Fatal("longpoll did not return after document was created")
	}
}

func TestDBChanges_FeedContinuous(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create initial document.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Start continuous feed WITHOUT include_docs.
	streamCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	received := make(chan model.ChangeResult, 10)
	go func() {
		_ = c.DB("testdb").ChangesContinuous(streamCtx, &model.ChangesOptions{Timeout: 3000 * time.Millisecond}, func(change model.ChangeResult) error {
			select {
			case received <- change:
			case <-streamCtx.Done():
			}
			return nil
		})
	}()

	// Read initial change (doc1).
	var initial model.ChangeResult
	select {
	case initial = <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("should receive initial change")
	}
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
	for {
		select {
		case change := <-received:
			if change.ID != "doc2" {
				continue
			}
			assert.Nil(t, change.Doc, "doc should be nil without include_docs")
			assert.NotEqual(t, "0", change.Seq, "live change seq should not be 0")
			return
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for live change — live changes may be dropped")
		}
	}
}

// TestDBChanges_FeedContinuous_IncludeDocs verifies that live changes in
// the continuous feed include the document body when include_docs=true
// and that the seq field is a real sequence number (not "0").
func TestDBChanges_FeedContinuous_IncludeDocs(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create initial document so the feed has something to start with.
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// include_docs isn't modeled by *client.Database's typed
	// ChangesContinuous (its model.ChangeResult.Doc field is json:"-"), so
	// stream raw and decode into the local ChangeDoc shape instead.
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := c.Raw(reqCtx, http.MethodGet, "/testdb/_changes?feed=continuous&include_docs=true&timeout=3000", nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

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
}

func TestDBChanges_Heartbeat(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a document first
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Test that heartbeat parameter is accepted and doesn't break normal
	// operation (feed=normal doesn't actually send heartbeats; this just
	// verifies the parameter is parsed and doesn't cause errors).
	result, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{
		Feed:      "normal",
		Heartbeat: 1000 * time.Millisecond,
	})
	require.NoError(t, err)

	// Should receive the document
	require.Len(t, result.Results, 1)
	assert.Equal(t, "doc1", result.Results[0].ID)
}

func TestDBChanges_Timeout(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Start longpoll with short timeout and no new changes
	start := time.Now()
	result, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{
		Feed:    "longpoll",
		Since:   "now",
		Timeout: 1000 * time.Millisecond,
	})
	elapsed := time.Since(start)
	require.NoError(t, err)

	// Should have waited approximately 1 second (timeout)
	// Allow some margin for test execution time (and real socket round trips).
	assert.Greater(t, elapsed, 800*time.Millisecond, "should wait at least 800ms")
	assert.Less(t, elapsed, 3*time.Second, "should not wait more than 3s")

	// Should return empty results after timeout
	assert.Empty(t, result.Results)
}

func TestDBChanges_DocIDsFilter(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesPOST(t, c, "testdb", "feed=normal", map[string]interface{}{
		"doc_ids": []string{"doc2", "doc4"},
	})

	// Should only receive doc2 and doc4
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "doc2")
	assert.Contains(t, docIDs, "doc4")
}

func TestDBChanges_Since(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create first document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	// Get current sequence
	result1, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal"})
	require.NoError(t, err)
	firstSeq := result1.LastSeq

	// Create second document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"value": 2},
	})
	require.NoError(t, err)

	// Get changes since first sequence
	result2, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal", Since: firstSeq})
	require.NoError(t, err)

	// Should only receive doc2
	require.Len(t, result2.Results, 1)
	assert.Equal(t, "doc2", result2.Results[0].ID)
}

func TestDBChanges_Since_LimitedEmptyPoll(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	result1, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal"})
	require.NoError(t, err)
	since := result1.LastSeq

	// A poll from the current (latest) sequence with a nonzero limit and no
	// new documents must echo back `since` as `last_seq`, not regress to "0".
	result2, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{
		Feed:  "normal",
		Since: since,
		Limit: 100,
	})
	require.NoError(t, err)

	assert.Empty(t, result2.Results)
	assert.Equal(t, since, result2.LastSeq)
}

// TestDBChanges_Since_RepeatedEmptyPollsStayStable guards against the
// checkpoint-oscillation failure mode described in the original bug report:
// a naive fix could still regress last_seq on, say, every *other* empty
// poll, or after some number of repeats. Chain several genuinely-empty
// polls in a row and assert last_seq never moves off since on any of them.
func TestDBChanges_Since_RepeatedEmptyPollsStayStable(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"value": 1}})
	require.NoError(t, err)

	result1, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal"})
	require.NoError(t, err)
	since := result1.LastSeq

	for i := 0; i < 5; i++ {
		result, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{
			Feed:  "normal",
			Since: since,
			Limit: 100,
		})
		require.NoError(t, err)
		require.Empty(t, result.Results, "poll %d should have found nothing new", i)
		require.Equal(t, since, result.LastSeq, "poll %d regressed last_seq", i)
	}
}

// TestDBChanges_Since_ExplicitLimitZeroEmptyPoll covers the explicit
// limit=0 query value specifically: the handler special-cases Limit==0 for
// an extra blank line in the response body (CouchDB compatibility), so it's
// worth confirming that formatting quirk doesn't interfere with last_seq
// still correctly echoing since when nothing new is found.
func TestDBChanges_Since_ExplicitLimitZeroEmptyPoll(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"value": 1}})
	require.NoError(t, err)

	result := rawChangesGET(t, c, "testdb", "feed=normal&limit=0")
	since := result.LastSeq
	require.NotEmpty(t, since)

	result2 := rawChangesGET(t, c, "testdb", "feed=normal&limit=0&since="+since)
	assert.Empty(t, result2.Results)
	assert.Equal(t, since, result2.LastSeq)
}

func TestDBChanges_Deleted(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal"})
	require.NoError(t, err)

	// Should receive both create and delete changes
	require.GreaterOrEqual(t, len(result.Results), 1)

	// Find the latest change for doc1
	var doc1Change *model.ChangeResult
	for i := range result.Results {
		if result.Results[i].ID == "doc1" {
			doc1Change = &result.Results[i]
		}
	}

	require.NotNil(t, doc1Change)
	assert.True(t, doc1Change.Deleted, "document should be marked as deleted")
}

func TestDBChanges_FeedEventSource(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create a document
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "doc1",
		Data: map[string]interface{}{"value": 1},
	})
	require.NoError(t, err)

	streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	received := make(chan model.ChangeResult, 1)
	go func() {
		_ = c.DB("testdb").ChangesEventSource(streamCtx, &model.ChangesOptions{}, func(change model.ChangeResult) error {
			select {
			case received <- change:
			default:
			}
			return nil
		})
	}()

	select {
	case change := <-received:
		assert.Equal(t, "doc1", change.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("should receive SSE formatted data")
	}
}

func TestDBChanges_FilterSelector(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesPOST(t, c, "testdb", "filter=_selector", map[string]interface{}{
		"selector": map[string]interface{}{
			"type": "post",
		},
	})

	// Should only receive posts
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "post1")
	assert.Contains(t, docIDs, "post2")
	assert.NotContains(t, docIDs, "comment1")
}

func TestDBChanges_FilterView(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesGET(t, c, "testdb", "filter=_view&view=blog/published")

	// Should only receive published posts (not design doc, not draft)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "post1", result.Results[0].ID)
}

func TestDBChanges_FilterCustom(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesGET(t, c, "testdb", "filter=app/important&minPriority=3")

	// Should only receive task1 and task3 (priority >= 3)
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "task1")
	assert.Contains(t, docIDs, "task3")
	assert.NotContains(t, docIDs, "task2")
}

func TestDBChanges_FilterCombinations(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesPOST(t, c, "testdb", "filter=_selector", map[string]interface{}{
		"doc_ids": []string{"doc2", "doc3", "doc4"},
		"selector": map[string]interface{}{
			"value": map[string]interface{}{
				"$gte": 3, // value >= 3
			},
		},
	})

	// Should only receive doc3 and doc4 (in doc_ids AND value >= 3)
	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "doc3")
	assert.Contains(t, docIDs, "doc4")
	assert.NotContains(t, docIDs, "doc2") // filtered out by selector
}

func TestDBChanges_EventSourceWithFilter(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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

	// Test EventSource with selector filter -- needs a POST body, which
	// *client.Database's typed ChangesEventSource (always GET) doesn't
	// support, so stream raw instead.
	bodyData := map[string]interface{}{
		"selector": map[string]interface{}{
			"type": "post",
		},
	}
	body, _ := json.Marshal(bodyData)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := c.Raw(reqCtx, http.MethodPost, "/testdb/_changes?feed=eventsource&filter=_selector", bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read SSE formatted output
	scanner := bufio.NewScanner(resp.Body)
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
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesGET(t, c, "testdb", "filter=_design/app/important&minPriority=3")

	// Should only receive task1 (priority >= 3)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "task1", result.Results[0].ID)
}

func TestDBChanges_FilterDesign(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal", Filter: "_design"})
	require.NoError(t, err)

	// Should only receive the design document
	require.Len(t, result.Results, 1)
	assert.Equal(t, "_design/myapp", result.Results[0].ID)
}

func TestDBChanges_FilterView_DesignPrefix(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesGET(t, c, "testdb", "filter=_view&view=_design/blog/published")

	// Should only receive published posts
	require.Len(t, result.Results, 1)
	assert.Equal(t, "post1", result.Results[0].ID)
}

func TestDBChanges_FilterCustom_UserCtx(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result, err := c.DB("testdb").Changes(ctx, &model.ChangesOptions{Feed: "normal", Filter: "app/by_user"})
	require.NoError(t, err)

	// Should only receive doc1 (authored by "admin")
	require.Len(t, result.Results, 1)
	assert.Equal(t, "doc1", result.Results[0].ID)
}

func TestDBChanges_FilterDocIDs_ExplicitFilter(t *testing.T) {
	s, c, cleanup := setupChangesTest(t)
	defer cleanup()

	ctx := t.Context()
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
	result := rawChangesPOST(t, c, "testdb", "filter=_doc_ids", map[string]interface{}{
		"doc_ids": []string{"doc1", "doc3"},
	})

	require.Len(t, result.Results, 2)
	docIDs := []string{result.Results[0].ID, result.Results[1].ID}
	assert.Contains(t, docIDs, "doc1")
	assert.Contains(t, docIDs, "doc3")
	assert.NotContains(t, docIDs, "doc2")
}
