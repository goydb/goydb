package reactor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/goydb"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/reactor"
	"github.com/stretchr/testify/require"
)

var errFailingHandler = errors.New("reactor_test: handler deliberately failed")

// setupReactorTest builds an ephemeral goydb instance and a "testdb"
// database, returning a *client.Client (paired to it via a Unix socketpair,
// see Goydb.NewLocalClient) and a cleanup func.
func setupReactorTest(t *testing.T) (*client.Client, func()) {
	t.Helper()

	cfg := &goydb.Config{
		DatabaseDir:     t.TempDir(),
		EnablePublicDir: false,
		CookieSecret:    strings.Repeat("ab", 32),
		Aministrators:   "admin:secret",
	}
	gdb, err := cfg.BuildDatabase()
	require.NoError(t, err)

	c, closeClient, err := gdb.NewLocalClient("test")
	require.NoError(t, err)

	require.NoError(t, c.DB("testdb").Create(context.Background()))

	return c, func() {
		_ = closeClient()
		_ = gdb.Close()
	}
}

func TestReactor_Basic(t *testing.T) {
	c, cleanup := setupReactorTest(t)
	defer cleanup()

	received := make(chan model.ChangeResult, 1)
	r := &reactor.Reactor{
		Client:   c,
		Database: "testdb",
		Name:     "basic",
		Handler: func(ctx context.Context, c *client.Client, change model.ChangeResult) error {
			select {
			case received <- change:
			default:
			}
			return nil
		},
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = r.Run(runCtx) }()

	time.Sleep(100 * time.Millisecond)
	_, err := c.DB("testdb").Put(context.Background(), &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	select {
	case change := <-received:
		require.Equal(t, "doc1", change.ID)
	case <-time.After(4 * time.Second):
		t.Fatal("handler did not observe the change in time")
	}
}

func TestReactor_CheckpointAdvances(t *testing.T) {
	c, cleanup := setupReactorTest(t)
	defer cleanup()
	ctx := context.Background()

	processed := make(chan model.ChangeResult, 1)
	r := &reactor.Reactor{
		Client:   c,
		Database: "testdb",
		Name:     "checkpoint",
		Handler: func(ctx context.Context, c *client.Client, change model.ChangeResult) error {
			processed <- change
			return nil
		},
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() { _ = r.Run(runCtx) }()

	time.Sleep(100 * time.Millisecond)
	_, err := c.DB("testdb").Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	var change model.ChangeResult
	select {
	case change = <-processed:
	case <-time.After(4 * time.Second):
		t.Fatal("handler did not observe the change in time")
	}

	// Give save() a moment to complete after the handler returned.
	require.Eventually(t, func() bool {
		doc, err := c.DB("testdb").Get(ctx, "_local/reactor-checkpoint")
		if err != nil {
			return false
		}
		since, _ := doc.Data["since"].(string)
		return since == change.Seq
	}, 2*time.Second, 50*time.Millisecond, "checkpoint must advance to the processed change's seq")
}

func TestReactor_ResumesFromCheckpoint(t *testing.T) {
	c, cleanup := setupReactorTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := c.DB("testdb").Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	firstSeen := make(chan model.ChangeResult, 1)
	r1 := &reactor.Reactor{
		Client:   c,
		Database: "testdb",
		Name:     "resume",
		Handler: func(ctx context.Context, c *client.Client, change model.ChangeResult) error {
			select {
			case firstSeen <- change:
			default:
			}
			return nil
		},
	}

	run1Ctx, cancel1 := context.WithCancel(ctx)
	done1 := make(chan struct{})
	go func() {
		_ = r1.Run(run1Ctx)
		close(done1)
	}()

	var firstChange model.ChangeResult
	select {
	case firstChange = <-firstSeen:
		require.Equal(t, "doc1", firstChange.ID)
	case <-time.After(4 * time.Second):
		t.Fatal("first reactor did not observe doc1 in time")
	}

	// Wait for save() to actually persist the checkpoint before cancelling --
	// the handler firing only means process() is about to call save(), not
	// that the checkpoint PUT has completed yet.
	require.Eventually(t, func() bool {
		doc, err := c.DB("testdb").Get(ctx, "_local/reactor-resume")
		if err != nil {
			return false
		}
		since, _ := doc.Data["since"].(string)
		return since == firstChange.Seq
	}, 2*time.Second, 50*time.Millisecond, "checkpoint must be saved before cancelling the first reactor")

	cancel1()
	<-done1

	_, err = c.DB("testdb").Put(ctx, &model.Document{ID: "doc2", Data: map[string]interface{}{}})
	require.NoError(t, err)

	secondSeen := make(chan model.ChangeResult, 10)
	r2 := &reactor.Reactor{
		Client:   c,
		Database: "testdb",
		Name:     "resume", // same name -- resumes from r1's checkpoint
		Handler: func(ctx context.Context, c *client.Client, change model.ChangeResult) error {
			secondSeen <- change
			return nil
		},
	}

	run2Ctx, cancel2 := context.WithTimeout(ctx, 3*time.Second)
	defer cancel2()
	go func() { _ = r2.Run(run2Ctx) }()

	select {
	case change := <-secondSeen:
		require.Equal(t, "doc2", change.ID, "second reactor must only see doc2, not reprocess doc1")
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("second reactor did not observe doc2 in time")
	}

	// Confirm doc1 is never delivered again within the remaining window.
	select {
	case change := <-secondSeen:
		t.Fatalf("second reactor unexpectedly reprocessed a change: %+v", change)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestReactor_HandlerErrorDoesNotAdvanceCheckpoint(t *testing.T) {
	c, cleanup := setupReactorTest(t)
	defer cleanup()
	ctx := context.Background()

	wantErr := errFailingHandler

	r := &reactor.Reactor{
		Client:   c,
		Database: "testdb",
		Name:     "failing",
		Handler: func(ctx context.Context, c *client.Client, change model.ChangeResult) error {
			return wantErr
		},
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- r.Run(runCtx) }()

	time.Sleep(100 * time.Millisecond)
	_, err := c.DB("testdb").Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{}})
	require.NoError(t, err)

	select {
	case err := <-runErr:
		require.Error(t, err)
		require.ErrorIs(t, err, wantErr)
	case <-time.After(4 * time.Second):
		t.Fatal("Run did not return after the handler failed")
	}

	_, err = c.DB("testdb").Get(ctx, "_local/reactor-failing")
	require.True(t, client.IsNotFound(err), "checkpoint must not be created when the handler fails")
}
