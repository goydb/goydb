package storage

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/require"
)

// TestChanges_IncrementalResumeAfterCheckpointDocUpdated covers the bug where
// resuming from a since cursor silently drops the next real change: if the
// document whose write produced the checkpoint sequence is updated again
// before the next poll, its changes-index entry at that exact sequence is
// gone (moved to a new, higher sequence), so Seek(since) lands directly on
// the next real change -- which must not then be skipped by an unconditional
// cursor.Next().
func TestChanges_IncrementalResumeAfterCheckpointDocUpdated(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "other-1", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{ID: "other-2", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{ID: "target", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	docs1, _, err := db.Changes(ctx, &model.ChangesOptions{Since: "", Limit: 1000})
	require.NoError(t, err)
	require.Len(t, docs1, 3)
	checkpoint := docs1[len(docs1)-1].LocalSeq
	require.Equal(t, "target", docs1[len(docs1)-1].ID, "test setup assumption: target produced the checkpoint sequence")

	// Update target again *after* the checkpoint was taken -- this removes
	// its old changes-index entry (at `checkpoint`) and inserts a new one at
	// a fresh, higher sequence.
	existing, err := db.GetDocument(ctx, "target")
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{ID: "target", Rev: existing.Rev, Data: map[string]interface{}{"n": 2}})
	require.NoError(t, err)

	docs2, _, err := db.Changes(ctx, &model.ChangesOptions{Since: strconv.FormatUint(checkpoint, 10), Limit: 1000})
	require.NoError(t, err)
	require.Len(t, docs2, 1, "target's update must be the one change returned, not silently dropped")
	require.Equal(t, "target", docs2[0].ID)
}

// TestChanges_IncrementalResumeCommonCase is the unaffected common path: the
// checkpoint document's own index entry is still present (never updated
// again), so Seek(since) lands exactly on it and must still be skipped --
// only documents strictly after it should come back.
func TestChanges_IncrementalResumeCommonCase(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "a", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{ID: "b", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	docs1, _, err := db.Changes(ctx, &model.ChangesOptions{Since: "", Limit: 1})
	require.NoError(t, err)
	require.Len(t, docs1, 1)
	require.Equal(t, "a", docs1[0].ID)
	checkpoint := docs1[0].LocalSeq

	docs2, _, err := db.Changes(ctx, &model.ChangesOptions{Since: strconv.FormatUint(checkpoint, 10), Limit: 1000})
	require.NoError(t, err)
	require.Len(t, docs2, 1, "must not repeat the checkpoint document itself")
	require.Equal(t, "b", docs2[0].ID)
}

// TestChanges_UpdateDoesNotDuplicateInFullRescan guards the related
// assumption the fix relies on: updating a document removes its stale
// changes-index entry rather than leaving both old and new entries behind.
func TestChanges_UpdateDoesNotDuplicateInFullRescan(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "target", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	existing, err := db.GetDocument(ctx, "target")
	require.NoError(t, err)
	_, err = db.PutDocument(ctx, &model.Document{ID: "target", Rev: existing.Rev, Data: map[string]interface{}{"n": 2}})
	require.NoError(t, err)

	docs, _, err := db.Changes(ctx, &model.ChangesOptions{Since: "", Limit: 1000})
	require.NoError(t, err)
	require.Len(t, docs, 1, "an updated document must appear once, not once per historical sequence")
}

// TestChanges_SinceNowWakesUpOnlyWithTheNewDocument covers the since=now
// (longpoll) wake-up path: the resume point after waking must be derived
// from a snapshot taken before waiting began, not from the triggering
// document's own LocalSeq -- that field is never populated on the
// write-path *model.Document NotifyDocumentUpdate passes to listeners, so
// deriving the resume point from it previously produced a bogus "since"
// value that returned every pre-existing document too.
func TestChanges_SinceNowWakesUpOnlyWithTheNewDocument(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	type result struct {
		docs []*model.Document
		err  error
	}
	done := make(chan result, 1)
	go func() {
		docs, _, err := db.Changes(ctx, &model.ChangesOptions{Since: "now", Limit: 1000, Timeout: 5 * time.Second})
		done <- result{docs, err}
	}()

	time.Sleep(100 * time.Millisecond)
	_, err = db.PutDocument(ctx, &model.Document{ID: "doc2", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.Len(t, r.docs, 1, "only the new document should be returned, not doc1 too")
		require.Equal(t, "doc2", r.docs[0].ID)
	case <-time.After(2 * time.Second):
		t.Fatal("since=now did not return after a document was created")
	}
}
