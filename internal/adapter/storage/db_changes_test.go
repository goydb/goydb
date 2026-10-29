package storage

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"github.com/stretchr/testify/assert"
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

// TestWaitForChange_WakesUpForRawTransactionWrite covers writes made through
// a caller-opened db.Transaction/port.DatabaseTx (as pkg/replication.LocalDB
// .BulkDocs and the attachment write paths do) rather than through the
// top-level PutDocument convenience method. Database.Transaction must
// announce every document written this way too, or a concurrent
// WaitForChange caller never wakes up even though the write is fully
// persisted and visible to ordinary reads.
func TestWaitForChange_WakesUpForRawTransactionWrite(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	type result struct {
		seq string
		err error
	}
	done := make(chan result, 1)
	go func() {
		seq, err := db.WaitForChange(ctx, "")
		done <- result{seq, err}
	}()

	time.Sleep(100 * time.Millisecond)
	err = db.Transaction(ctx, func(tx port.DatabaseTx) error {
		_, err := tx.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"n": 1}})
		return err
	})
	require.NoError(t, err)

	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.NotEmpty(t, r.seq)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForChange did not wake up for a document written through a raw db.Transaction/port.DatabaseTx call")
	}

	// GetDocument must agree: the write landed regardless of notification.
	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

// TestWaitForChange_WakesUpForConflictLeafDelete covers tx.DeleteDocument's
// conflict-leaf branch specifically: deleting a non-winning leaf revision
// builds a tombstone without delegating to tx.PutDocument (unlike deleting
// the winning revision), so it needs its own notification tracking. This is
// the one path the BulkDocs/attachment regression tests don't exercise. The
// delete is made through a raw db.Transaction/port.DatabaseTx call -- not
// the top-level Database.DeleteDocument, which already notified
// unconditionally before this fix and so wouldn't catch a regression here.
func TestWaitForChange_WakesUpForConflictLeafDelete(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	// Create two conflicting leaves for doc1 via PutDocumentForReplication
	// (new_edits=false), mirroring how replication produces conflicts.
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID: "doc1", Rev: "1-aaa", Data: map[string]interface{}{"_id": "doc1", "_rev": "1-aaa"},
	})
	require.NoError(t, err)
	err = db.PutDocumentForReplication(ctx, &model.Document{
		ID: "doc1", Rev: "1-zzz", Data: map[string]interface{}{"_id": "doc1", "_rev": "1-zzz"},
	})
	require.NoError(t, err)

	leaves, err := db.GetLeaves(ctx, "doc1")
	require.NoError(t, err)
	require.Len(t, leaves, 2, "test setup assumption: doc1 should have two conflicting leaves")

	winner, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	nonWinnerRev := "1-aaa"
	if winner.Rev == nonWinnerRev {
		nonWinnerRev = "1-zzz"
	}

	since, err := db.WaitForChange(ctx, "")
	require.NoError(t, err)

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
	err = db.Transaction(ctx, func(tx port.DatabaseTx) error {
		_, err := tx.DeleteDocument(ctx, "doc1", nonWinnerRev)
		return err
	})
	require.NoError(t, err)

	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.NotEmpty(t, r.seq)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForChange did not wake up for a conflict-leaf delete made through a raw db.Transaction call")
	}
}

// TestDatabaseTransaction_RollbackDoesNotNotify is the flip side of the
// raw-transaction notification fix: when the callback passed to
// Database.Transaction returns an error -- so bbolt never commits the
// writes made inside it -- no NotifyDocumentUpdate must fire for them.
// Database.Transaction only drains the tracked notifications when the
// transaction's own error is nil; getting that guard wrong would mean
// listeners fire for documents that were never actually persisted.
func TestDatabaseTransaction_RollbackDoesNotNotify(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	notified := make(chan string, 10)
	err = db.AddListener(ctx, port.ChangeListenerFunc(func(_ context.Context, doc *model.Document) error {
		notified <- doc.ID
		return nil
	}))
	require.NoError(t, err)

	boom := errors.New("boom")
	err = db.Transaction(ctx, func(tx port.DatabaseTx) error {
		_, err := tx.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"n": 1}})
		if err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	// The write must not have been persisted...
	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	require.Nil(t, doc, "a rolled-back transaction's writes must not be persisted")

	// ...and must not have notified listeners either.
	select {
	case id := <-notified:
		t.Fatalf("listener was notified for %q from a transaction that returned an error and rolled back", id)
	case <-time.After(300 * time.Millisecond):
	}

	// Sanity check: the listener registration itself still works for a real,
	// committed write, so the absence above isn't just a broken listener.
	_, err = db.PutDocument(ctx, &model.Document{ID: "doc2", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)
	select {
	case id := <-notified:
		require.Equal(t, "doc2", id)
	case <-time.After(2 * time.Second):
		t.Fatal("listener never fired for a real, committed write either -- test setup is broken")
	}
}

// TestTransaction_DeleteDocument_NotifiesExactlyOnce guards against
// reintroducing a double notification on tx.DeleteDocument's delegate path:
// deleting the winning revision delegates to tx.PutDocument, which already
// tracks its own notification, so DeleteDocument itself must not track the
// same document a second time.
//
// The listener is registered before any write (including the setup write
// that creates doc1) so there's no ordering race between that write's own
// asynchronous NotifyDocumentUpdate dispatch and AddListener: every
// notification this test's write traffic can produce arrives on the
// channel, in some order, and the test drains the ones it expects before
// checking that no extra one follows.
func TestTransaction_DeleteDocument_NotifiesExactlyOnce(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	notified := make(chan *model.Document, 10)
	err = db.AddListener(ctx, port.ChangeListenerFunc(func(_ context.Context, doc *model.Document) error {
		if doc.ID == "doc1" {
			notified <- doc
		}
		return nil
	}))
	require.NoError(t, err)

	rev, err := db.PutDocument(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)

	// Drain the creation's own notification before testing the delete.
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("did not observe the notification for doc1's creation")
	}

	err = db.Transaction(ctx, func(tx port.DatabaseTx) error {
		_, err := tx.DeleteDocument(ctx, "doc1", rev)
		return err
	})
	require.NoError(t, err)

	// Exactly one notification for the delete...
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("delete did not notify listeners")
	}

	// ...and no extra (duplicate) notification behind it.
	select {
	case extra := <-notified:
		t.Fatalf("got an unexpected extra notification for %q (rev %q) -- delete notified more than once", extra.ID, extra.Rev)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestDatabaseTransaction_EmptyTransactionDoesNotNotify covers the zero-write
// edge case: a Transaction callback that succeeds without writing or
// deleting any document (e.g. a read-only lookup, or a no-op branch in a
// bulk operation) must not notify listeners at all -- tx.notifications
// should simply stay nil/empty, not panic or spuriously fire.
func TestDatabaseTransaction_EmptyTransactionDoesNotNotify(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	notified := make(chan string, 10)
	err = db.AddListener(ctx, port.ChangeListenerFunc(func(_ context.Context, doc *model.Document) error {
		notified <- doc.ID
		return nil
	}))
	require.NoError(t, err)

	err = db.Transaction(ctx, func(tx port.DatabaseTx) error {
		_, err := tx.GetDocument(ctx, "does-not-exist")
		return err
	})
	require.NoError(t, err)

	select {
	case id := <-notified:
		t.Fatalf("listener was notified for %q from a transaction that wrote nothing", id)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestDatabaseTransaction_NotifiesEachWriteToSameDocumentInOneTransaction
// covers writing the same document ID twice within a single transaction
// (e.g. a bulk operation that happens to touch one ID more than once): both
// writes are real, successive edits, so both must notify -- the
// accumulation in Transaction.notifications must not dedupe by document ID.
func TestDatabaseTransaction_NotifiesEachWriteToSameDocumentInOneTransaction(t *testing.T) {
	s, cleanup := openStorage(t)
	defer cleanup()

	ctx := context.Background()
	db, err := s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	notified := make(chan *model.Document, 10)
	err = db.AddListener(ctx, port.ChangeListenerFunc(func(_ context.Context, doc *model.Document) error {
		if doc.ID == "doc1" {
			notified <- doc
		}
		return nil
	}))
	require.NoError(t, err)

	err = db.Transaction(ctx, func(tx port.DatabaseTx) error {
		doc := &model.Document{ID: "doc1", Data: map[string]interface{}{"n": 1}}
		if _, err := tx.PutDocument(ctx, doc); err != nil {
			return err
		}
		// doc.Rev now holds the first write's generated revision. Reuse it
		// directly rather than reading it back via tx.GetDocument: bbolt
		// defers writes to a separate commit phase, so they aren't visible
		// to reads within the same transaction (see PutDocumentForReplication's
		// comment on this same quirk).
		_, err := tx.PutDocument(ctx, &model.Document{ID: "doc1", Rev: doc.Rev, Data: map[string]interface{}{"n": 2}})
		return err
	})
	require.NoError(t, err)

	var revs []string
	for i := 0; i < 2; i++ {
		select {
		case doc := <-notified:
			revs = append(revs, doc.Rev)
		case <-time.After(2 * time.Second):
			t.Fatalf("only got %d of 2 expected notifications", i)
		}
	}
	require.Len(t, revs, 2)
	assert.NotEqual(t, revs[0], revs[1], "the two writes produced different revisions and both must be reported")

	select {
	case extra := <-notified:
		t.Fatalf("got an unexpected 3rd notification for doc1 (rev %q)", extra.Rev)
	case <-time.After(300 * time.Millisecond):
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
