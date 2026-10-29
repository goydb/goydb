package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBDocGet_RevisionsOnlyWithRevsParam(t *testing.T) {
	c, _, cleanup := setupClientTest(t)
	defer cleanup()
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	// Create a document
	_, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"name": "test", "value": 123}})
	require.NoError(t, err)

	// Test 1: GET without revs parameter should NOT include _revisions
	doc, err := db.Get(ctx, "doc1")
	require.NoError(t, err)
	assert.NotEmpty(t, doc.ID)
	assert.NotEmpty(t, doc.Rev)
	assert.Nil(t, doc.Data["_revisions"], "_revisions should NOT be present without revs=true")

	// Test 2: GET with revs=true should include _revisions
	doc, err = db.Get(ctx, "doc1", client.WithRevs(true))
	require.NoError(t, err)
	assert.NotNil(t, doc.Data["_revisions"], "_revisions should be present with revs=true")

	// Test 3: Subsequent GET without revs should NOT include _revisions
	doc, err = db.Get(ctx, "doc1")
	require.NoError(t, err)
	assert.Nil(t, doc.Data["_revisions"], "_revisions should NOT persist after revs=true request")
}

func TestDBDocGet_RevisionsChainGrowsWithUpdates(t *testing.T) {
	c, _, cleanup := setupClientTest(t)
	defer cleanup()
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	rev1, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"x": 1}})
	require.NoError(t, err)

	_, err = db.Put(ctx, &model.Document{ID: "doc1", Rev: rev1, Data: map[string]interface{}{"x": 2}})
	require.NoError(t, err)

	doc, err := db.Get(ctx, "doc1", client.WithRevs(true))
	require.NoError(t, err)

	revisions, ok := doc.Data["_revisions"].(map[string]interface{})
	require.True(t, ok, "_revisions should be present with revs=true")

	start, ok := revisions["start"].(float64)
	require.True(t, ok, "start should be a number")
	assert.Equal(t, float64(2), start, "start should be 2 after two updates")

	ids, ok := revisions["ids"].([]interface{})
	require.True(t, ok, "ids should be an array")
	assert.Len(t, ids, 2, "should have 2 revision IDs in the chain")
}

// setupRevisionTest is setupClientTest, kept under its original name since
// many tests in this file already call it.
func setupRevisionTest(t *testing.T) (*storage.Storage, *client.Client, func()) {
	t.Helper()
	c, s, cleanup := setupClientTest(t)
	return s, c, cleanup
}

func TestDocGet_ConflictsField(t *testing.T) {
	s, c, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz"},
	})
	require.NoError(t, err)

	cdb := c.DB("testdb")

	// Without ?conflicts=true, _conflicts must NOT be in the response (CouchDB compat).
	doc, err := cdb.Get(ctx, "doc1")
	require.NoError(t, err)
	assert.Nil(t, doc.Data["_conflicts"], "_conflicts should NOT be present without ?conflicts=true")

	// With ?conflicts=true, _conflicts MUST be present.
	doc, err = cdb.Get(ctx, "doc1", client.WithConflicts(true))
	require.NoError(t, err)
	assert.NotNil(t, doc.Data["_conflicts"], "_conflicts should be present with ?conflicts=true")
	conflictList, ok := doc.Data["_conflicts"].([]interface{})
	require.True(t, ok, "_conflicts should be an array")
	assert.Len(t, conflictList, 1, "should have 1 conflict revision")
}

// getOpenRevs issues GET /{db}/{docid}?open_revs=... via Raw (pkg/client's
// typed Get doesn't expose the open_revs multi-result array shape) and
// decodes the JSON array response.
func getOpenRevs(t *testing.T, c *client.Client, dbName, docID, openRevsJSON string) (code int, result []map[string]interface{}) {
	t.Helper()
	path := "/" + docID + "?open_revs=" + url.QueryEscape(openRevsJSON)
	resp, err := c.DB(dbName).Raw(t.Context(), http.MethodGet, path, nil, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func TestDocGet_OpenRevsAll(t *testing.T) {
	s, c, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz"},
	})
	require.NoError(t, err)

	code, result := getOpenRevs(t, c, "testdb", "doc1", `all`)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, result, 2)
}

func TestDocGet_OpenRevsSpecific(t *testing.T) {
	s, c, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz"},
	})
	require.NoError(t, err)

	code, result := getOpenRevs(t, c, "testdb", "doc1", `["1-aaa"]`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result, 1)
	assert.NotNil(t, result[0]["ok"])
}

func TestDocGet_OpenRevsMissingRev(t *testing.T) {
	s, c, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"x": 1}})
	require.NoError(t, err)

	code, result := getOpenRevs(t, c, "testdb", "doc1", `["3-nonexistent"]`)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, result, 1)
	assert.Equal(t, "3-nonexistent", result[0]["missing"])
}

func TestAllDocs_NoRevisions(t *testing.T) {
	c, _, cleanup := setupClientTest(t)
	defer cleanup()
	ctx := context.Background()
	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))

	// Create a document
	_, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"name": "test"}})
	require.NoError(t, err)

	// Query _all_docs with include_docs=true
	result, err := db.AllDocs(ctx, client.AllDocsQuery{IncludeDocs: true})
	require.NoError(t, err)

	require.Len(t, result.Rows, 1)
	doc := result.Rows[0].Doc

	assert.NotNil(t, doc["_id"])
	assert.NotNil(t, doc["_rev"])
	assert.Equal(t, "test", doc["name"])
	assert.Nil(t, doc["_revisions"], "_revisions should NOT be in _all_docs response")
}

func TestDocGet_RevParam_WinningRevision(t *testing.T) {
	s, c, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"x": 1}})
	require.NoError(t, err)

	// ?rev= matching the winning revision should return the document normally.
	doc, err := c.DB("testdb").Get(ctx, "doc1", client.WithRev(rev))
	require.NoError(t, err)
	assert.Equal(t, "doc1", doc.ID)
	assert.Equal(t, rev, doc.Rev)
}

func TestDocGet_RevParam_ConflictRevision(t *testing.T) {
	s, c, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create two conflicting revisions via replication.
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-aaa",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa", "source": "replica-a"},
	})
	require.NoError(t, err)

	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID:   "doc1",
		Rev:  "1-zzz",
		Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz", "source": "replica-z"},
	})
	require.NoError(t, err)

	// Winner is 1-zzz (higher lexicographic hash). Fetch the loser 1-aaa via ?rev=.
	doc, err := c.DB("testdb").Get(ctx, "doc1", client.WithRev("1-aaa"))
	require.NoError(t, err)
	assert.Equal(t, "doc1", doc.ID)
	assert.Equal(t, "1-aaa", doc.Rev)
	assert.Equal(t, "replica-a", doc.Data["source"])
}

func TestDocGet_RevParam_NonExistent(t *testing.T) {
	s, c, cleanup := setupRevisionTest(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"x": 1}})
	require.NoError(t, err)

	// ?rev= with a non-existent revision should return 404.
	_, err = c.DB("testdb").Get(ctx, "doc1", client.WithRev("99-nonexistent"))
	require.Error(t, err)
	assert.True(t, client.IsNotFound(err))
}
