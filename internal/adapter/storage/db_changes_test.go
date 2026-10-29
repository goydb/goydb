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
		// Feed: "longpoll" matters here, not just for realism (the HTTP
		// handler always sets Feed explicitly): it's what makes a spurious
		// empty wake-up retry instead of returning early. A wake can be
		// spurious even when correct -- NotifyDocumentUpdate dispatches to
		// listeners from an async goroutine with no ordering guarantee
		// against a listener registered moments after an earlier write
		// (here, doc1's own notification can arrive after this listener
		// registers), and the fix doesn't try to distinguish "real" wakes
		// from that: it just re-snapshots and waits again.
		docs, _, err := db.Changes(ctx, &model.ChangesOptions{Since: "now", Limit: 1000, Timeout: 5 * time.Second, Feed: "longpoll"})
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

// TestWaitForChange_ReturnsImmediatelyWhenAlreadyPast covers the cheap
// no-wait path: if a qualifying change already exists, WaitForChange must
// not block at all.
func TestWaitForChange_ReturnsImmediatelyWhenAlreadyPast(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	done := make(chan struct{})
	var seq string
	go func() {
		defer close(done)
		seq, err = db.WaitForChange(ctx, "")
	}()

	select {
	case <-done:
		require.NoError(t, err)
		require.NotEmpty(t, seq)
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForChange did not return immediately despite an existing change")
	}
}

// TestWaitForChange_WakesUpOnlyWithTheNewDocument mirrors
// TestChanges_SinceNowWakesUpOnlyWithTheNewDocument: resuming from the
// sequence of an existing document must wake up (only) on the next new one.
func TestWaitForChange_WakesUpOnlyWithTheNewDocument(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	_, err = db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	docs, _, err := db.Changes(ctx, &model.ChangesOptions{Since: "", Limit: 1000})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	since := strconv.FormatUint(docs[0].LocalSeq, 10)

	type result struct {
		seq string
		err error
	}
	done := make(chan result, 1)
	go func() {
		seq, err := db.WaitForChange(ctx, since)
		done <- result{seq, err}
	}()

	time.Sleep(100 * time.Millisecond)
	_, err = db.PutDocument(ctx, &model.Document{ID: "doc2", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.True(t, seqGreater(r.seq, since), "returned sequence must be past the given since")
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForChange did not return after a document was created")
	}
}

// TestWaitForChange_ContextCancellationReturnsPromptly ensures cancelling
// the caller's context unblocks a pending wait immediately with ctx.Err(),
// rather than waiting for a change or a timeout.
func TestWaitForChange_ContextCancellationReturnsPromptly(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	waitCtx, cancel := context.WithCancel(ctx)

	type result struct {
		seq string
		err error
	}
	done := make(chan result, 1)
	go func() {
		seq, err := db.WaitForChange(waitCtx, "")
		done <- result{seq, err}
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case r := <-done:
		require.ErrorIs(t, r.err, context.Canceled)
		require.Empty(t, r.seq)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForChange did not return promptly after ctx cancellation")
	}
}
