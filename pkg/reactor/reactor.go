// Package reactor watches a database's changes feed and calls a handler for
// each change, persisting progress as a checkpoint document in that same
// database so it resumes correctly after a restart instead of reprocessing
// everything from the beginning.
package reactor

import (
	"context"
	"fmt"

	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
)

// Handler processes one change event. The client is provided so the
// handler can fetch the full document (model.ChangeResult doesn't carry
// one), write to other databases, etc. Returning an error stops Run before
// the checkpoint for this change is persisted, so a later call to Run
// retries this same change -- at-least-once delivery, never silently lost.
type Handler func(ctx context.Context, c *client.Client, change model.ChangeResult) error

// Reactor watches one database's changes feed and calls Handler for each
// change, persisting progress as a _local checkpoint document in that same
// database so a later Run call resumes where the previous one left off.
//
// Client needs a transport that can hold more than one connection open at
// once: Run keeps a long-lived streaming GET open for its entire lifetime
// while also issuing a checkpoint PUT after every change on that same
// Client. A Client from pkg/client.NewPaired or Goydb.NewLocalClient
// supports this (each connection gets its own fresh socketpair -- see
// pkg/unixhttp.Pair), as does a Client backed by a real listening socket
// (TCP, a named Unix socket via pkg/unixhttp.Listen, or a real remote
// server).
type Reactor struct {
	Client   *client.Client
	Database string
	Handler  Handler

	// Name distinguishes this reactor's checkpoint from any other reactor
	// watching the same database. Defaults to "default".
	Name string

	// StartFromNow, when true, makes a *first* run (no checkpoint exists
	// yet) start from the current end of the feed instead of from the very
	// beginning. Ignored once a checkpoint exists.
	StartFromNow bool
}

// Run watches Database's changes feed and blocks until ctx is cancelled,
// Handler returns an error (wrapped and returned by Run), or the feed
// connection closes on its own. Run makes a single attempt -- it does not
// retry or reconnect; callers wanting that call Run again (checkpointed
// progress means a retry resumes correctly, not from the beginning).
func (r *Reactor) Run(ctx context.Context) error {
	db := r.Client.DB(r.Database)

	cp, err := r.loadCheckpoint(ctx, db)
	if err != nil {
		return err
	}

	opts := &model.ChangesOptions{Since: cp.since}
	process := func(change model.ChangeResult) error {
		if err := r.Handler(ctx, r.Client, change); err != nil {
			return fmt.Errorf("reactor: handler: %w", err)
		}
		return r.save(ctx, db, cp, change.Seq)
	}

	return db.ChangesContinuous(ctx, opts, process)
}

func (r *Reactor) checkpointID() string {
	name := r.Name
	if name == "" {
		name = "default"
	}
	return "_local/reactor-" + name
}

// checkpoint tracks the in-flight progress document across the lifetime of
// one Run call: doc carries the current _rev forward so each save() is a
// correct update rather than a stale-rev conflict.
type checkpoint struct {
	doc   *model.Document
	since string
}

func (r *Reactor) loadCheckpoint(ctx context.Context, db *client.Database) (*checkpoint, error) {
	doc, err := db.Get(ctx, r.checkpointID())
	if err != nil {
		if !client.IsNotFound(err) {
			return nil, fmt.Errorf("reactor: loading checkpoint: %w", err)
		}
		since := ""
		if r.StartFromNow {
			since = "now"
		}
		return &checkpoint{
			doc:   &model.Document{ID: r.checkpointID(), Data: map[string]interface{}{}},
			since: since,
		}, nil
	}
	since, _ := doc.Data["since"].(string)
	return &checkpoint{doc: doc, since: since}, nil
}

func (r *Reactor) save(ctx context.Context, db *client.Database, cp *checkpoint, seq string) error {
	cp.doc.Data["since"] = seq
	rev, err := db.Put(ctx, cp.doc)
	if err != nil {
		return fmt.Errorf("reactor: saving checkpoint: %w", err)
	}
	cp.doc.Rev = rev
	cp.since = seq
	return nil
}
