package controller

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const replicatorDBName = "_replicator"

// Replication watches the _replicator database and manages replication jobs
type Replication struct {
	Storage port.Storage
	Service *ReplicationService

	mu                 sync.Mutex
	trigger            chan struct{}
	listenerRegistered bool
}

// Run polls the _replicator database every 5 seconds and also reacts
// immediately when a document is written to _replicator.
func (c *Replication) Run(ctx context.Context) {
	c.trigger = make(chan struct{}, 1)

	c.tryRegisterListener(ctx)

	// Poll once immediately on startup rather than waiting for the first tick.
	c.processReplicatorDB(ctx)

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			c.Service.CancelAll()
			return
		case <-c.trigger:
			c.processReplicatorDB(ctx)
		case <-t.C:
			c.processReplicatorDB(ctx)
		}
	}
}

// tryRegisterListener hooks a change listener onto the _replicator database so
// that writes wake the controller immediately. It is a no-op if the database
// does not exist yet or if the listener was already registered.
func (c *Replication) tryRegisterListener(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listenerRegistered {
		return
	}
	db, err := c.Storage.Database(ctx, replicatorDBName)
	if err != nil {
		return
	}
	_ = db.AddListener(ctx, port.ChangeListenerFunc(func(_ context.Context, _ *model.Document) error {
		select {
		case c.trigger <- struct{}{}:
		default:
		}
		return nil
	}))
	c.listenerRegistered = true
}

func (c *Replication) processReplicatorDB(ctx context.Context) {
	// Register listener now if _replicator was created after startup.
	c.tryRegisterListener(ctx)

	db, err := c.Storage.Database(ctx, replicatorDBName)
	if err != nil {
		return // _replicator doesn't exist yet, nothing to do
	}

	docs, _, err := db.AllDocs(ctx, port.AllDocsQuery{
		IncludeDocs: true,
		SkipLocal:   true,
	})
	if err != nil {
		log.Printf("Replication controller: failed to list docs: %v", err)
		return
	}

	// Track which replication IDs are still present
	activeIDs := make(map[string]bool)

	for _, doc := range docs {
		// Skip design docs
		if doc.IsDesignDoc() {
			continue
		}
		// Skip local docs
		if doc.IsLocalDoc() {
			continue
		}

		repDoc, err := model.ParseReplicationDoc(doc)
		if err != nil {
			log.Printf("replication controller: skipping %q: %v", doc.ID, err)
			continue
		}

		// Skip non-continuous replications that already finished successfully.
		if repDoc.ReplicationState == model.ReplicationStateCompleted && !repDoc.Continuous {
			continue
		}
		// Skip permanently-failed replications; user can inspect and recreate.
		if repDoc.ReplicationState == model.ReplicationStateError {
			continue
		}

		activeIDs[repDoc.ID] = true

		if c.Service.IsRunning(repDoc.ID) {
			continue
		}

		c.Service.Submit(ctx, repDoc, func(state model.ReplicationState, reason string) {
			c.updateState(ctx, db, repDoc, state, reason)
		})
	}

	// Cancel replications whose docs have been deleted
	c.Service.CancelStaleJobs(activeIDs)
}

func (c *Replication) updateState(ctx context.Context, db port.Database, repDoc *model.ReplicationDoc, state model.ReplicationState, reason string) {
	doc, err := db.GetDocument(ctx, repDoc.ID)
	if err != nil || doc == nil {
		return
	}

	doc.Data["_replication_state"] = string(state)
	doc.Data["_replication_state_time"] = time.Now().Format(time.RFC3339)
	if reason != "" {
		doc.Data["_replication_state_reason"] = reason
	} else {
		delete(doc.Data, "_replication_state_reason")
	}

	_, err = db.PutDocument(ctx, doc)
	if err != nil {
		log.Printf("Failed to update replication state for %s: %v", repDoc.ID, err)
	}
}
