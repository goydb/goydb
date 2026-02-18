package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	adapterreplication "github.com/goydb/goydb/internal/adapter/replication"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const replicatorDBName = "_replicator"

const (
	initialRetryDelay = 5 * time.Second
	maxRetryDelay     = 60 * time.Second
	maxRetryCount     = 10 // After 10 consecutive failures, give up
)

// computeRepID generates the replication ID (MD5 hash of source+target)
// to match the repID used by the controller layer
func computeRepID(source, target string) string {
	h := md5.New()
	h.Write([]byte(source))
	h.Write([]byte(target))
	return hex.EncodeToString(h.Sum(nil))
}

// Replication watches the _replicator database and manages replication jobs
type Replication struct {
	Storage port.Storage
	Logger  port.Logger

	// DB watcher fields
	mu                 sync.Mutex
	trigger            chan struct{}
	listenerRegistered bool

	// Job manager fields
	active map[string]context.CancelFunc
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
			c.CancelAll()
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
		c.Logger.Warnf(ctx, "failed to list replicator docs", "error", err)
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
			c.Logger.Warnf(ctx, "skipping invalid replication doc", "docID", doc.ID, "error", err)
			continue
		}

		repID := computeRepID(repDoc.Source, repDoc.Target)

		// Skip non-continuous replications that already finished successfully.
		if repDoc.ReplicationState == model.ReplicationStateCompleted && !repDoc.Continuous {
			continue
		}

		// Allow retrying failed replications with backoff
		if repDoc.ReplicationState == model.ReplicationStateError {
			// Check if enough time has passed since last retry
			if !c.shouldRetry(repDoc) {
				continue // Still in backoff period or max retries exceeded
			}
			c.Logger.Infof(ctx, "retrying failed replication", "docID", repDoc.ID, "repID", repID, "retry_count", repDoc.ConsecutiveFails)
		}

		// Also handle crashing state (transient failures)
		if repDoc.ReplicationState == model.ReplicationStateCrashing {
			if !c.shouldRetry(repDoc) {
				continue // Still in backoff period
			}
			c.Logger.Infof(ctx, "retrying crashing replication", "docID", repDoc.ID, "repID", repID, "retry_count", repDoc.ConsecutiveFails)
		}

		activeIDs[repDoc.ID] = true

		if c.IsRunning(repDoc.ID) {
			continue
		}

		c.Submit(ctx, repDoc, func(state model.ReplicationState, reason string) {
			c.updateState(ctx, db, repDoc, state, reason)
		})
	}

	// Cancel replications whose docs have been deleted
	c.CancelStaleJobs(activeIDs)
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
		repID := computeRepID(repDoc.Source, repDoc.Target)
		c.Logger.Warnf(ctx, "failed to update replication state", "docID", repDoc.ID, "repID", repID, "error", err)
	}
}

// shouldRetry determines if a failed replication should be retried now
// based on exponential backoff. Returns true if enough time has passed.
func (c *Replication) shouldRetry(repDoc *model.ReplicationDoc) bool {
	// If never retried or no last retry time, allow immediately
	if repDoc.LastRetryTime.IsZero() {
		return true
	}

	// Check if we've exceeded max retries
	if repDoc.ConsecutiveFails >= maxRetryCount {
		return false
	}

	// Calculate backoff: 5s, 10s, 20s, 40s, 60s (capped)
	retryDelay := initialRetryDelay * (1 << uint(repDoc.ConsecutiveFails))
	if retryDelay > maxRetryDelay {
		retryDelay = maxRetryDelay
	}

	// Check if enough time has passed
	nextRetry := repDoc.LastRetryTime.Add(retryDelay)
	return time.Now().After(nextRetry)
}

// incrementRetryCount updates the replication doc with incremented retry metadata
// AND sets the state to crashing (or error if max retries exceeded).
// This is done in a single write to avoid race conditions with the change listener.
func (c *Replication) incrementRetryCount(ctx context.Context, repDoc *model.ReplicationDoc, reason string) {
	repID := computeRepID(repDoc.Source, repDoc.Target)

	db, err := c.Storage.Database(ctx, replicatorDBName)
	if err != nil {
		return
	}

	doc, err := db.GetDocument(ctx, repDoc.ID)
	if err != nil || doc == nil {
		return
	}

	// Read CURRENT value from document, not stale repDoc
	consecutiveFails := 0
	if fails, ok := doc.Data["_replication_consecutive_fails"].(float64); ok {
		consecutiveFails = int(fails)
	}
	consecutiveFails++

	doc.Data["_replication_consecutive_fails"] = consecutiveFails
	doc.Data["_replication_last_retry"] = time.Now().Format(time.RFC3339)
	doc.Data["_replication_state_time"] = time.Now().Format(time.RFC3339)

	// After max retries, move to permanent error state
	if consecutiveFails >= maxRetryCount {
		doc.Data["_replication_state"] = string(model.ReplicationStateError)
		doc.Data["_replication_state_reason"] = "max retries exceeded"
		c.Logger.Warnf(ctx, "replication max retries exceeded, marking as error", "docID", repDoc.ID, "repID", repID)
	} else {
		// Set crashing state
		doc.Data["_replication_state"] = string(model.ReplicationStateCrashing)
		if reason != "" {
			doc.Data["_replication_state_reason"] = reason
		}
	}

	_, err = db.PutDocument(ctx, doc)
	if err != nil {
		c.Logger.Warnf(ctx, "failed to update retry metadata", "docID", repDoc.ID, "repID", repID, "error", err)
	}
}

// resetRetryCount clears retry metadata after successful replication
func (c *Replication) resetRetryCount(ctx context.Context, repDoc *model.ReplicationDoc) {
	if repDoc.ConsecutiveFails == 0 {
		return // Nothing to reset
	}

	repID := computeRepID(repDoc.Source, repDoc.Target)

	db, err := c.Storage.Database(ctx, replicatorDBName)
	if err != nil {
		return
	}

	doc, err := db.GetDocument(ctx, repDoc.ID)
	if err != nil || doc == nil {
		return
	}

	delete(doc.Data, "_replication_consecutive_fails")
	delete(doc.Data, "_replication_last_retry")

	_, err = db.PutDocument(ctx, doc)
	if err != nil {
		c.Logger.Debugf(ctx, "failed to reset retry metadata", "docID", repDoc.ID, "repID", repID, "error", err)
	}
}

// BuildPeer is the single source of truth for creating a port.ReplicationPeer
// from an address string. HTTP(S) addresses become remote clients; everything
// else is treated as a local database name. Custom headers can be provided for
// authentication or other purposes.
func (c *Replication) BuildPeer(addr string, customHeaders map[string]string) port.ReplicationPeer {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		client, err := adapterreplication.NewRemoteClient(addr, customHeaders)
		if err != nil {
			c.Logger.Errorf(context.Background(), "failed to create remote peer", "address", addr, "error", err)
			return nil
		}
		return client
	}
	return &adapterreplication.LocalDB{
		Storage: c.Storage,
		DBName:  addr,
	}
}

// RunSync executes a replication synchronously and returns the result. Used by the
// POST /_replicate HTTP handler.
func (c *Replication) RunSync(ctx context.Context, repDoc *model.ReplicationDoc) (*model.ReplicationResult, error) {
	source := c.BuildPeer(repDoc.Source, repDoc.SourceHeaders)
	target := c.BuildPeer(repDoc.Target, repDoc.TargetHeaders)
	if source == nil || target == nil {
		return nil, fmt.Errorf("invalid source or target")
	}
	return controller.NewReplicator(source, target, repDoc, c.Logger).Run(ctx)
}

// Submit starts an async replication goroutine for a _replicator-DB job.
// onState is called on each state transition so the caller can persist the
// state back to the document.
func (c *Replication) Submit(ctx context.Context, repDoc *model.ReplicationDoc, onState func(model.ReplicationState, string)) {
	source := c.BuildPeer(repDoc.Source, repDoc.SourceHeaders)
	target := c.BuildPeer(repDoc.Target, repDoc.TargetHeaders)
	if source == nil || target == nil {
		onState(model.ReplicationStateError, "invalid source or target")
		return
	}

	replicator := controller.NewReplicator(source, target, repDoc, c.Logger)
	repCtx, cancel := context.WithCancel(ctx)
	repID := computeRepID(repDoc.Source, repDoc.Target)

	c.mu.Lock()
	if c.active == nil {
		c.active = make(map[string]context.CancelFunc)
	}
	c.active[repDoc.ID] = cancel
	c.mu.Unlock()

	onState(model.ReplicationStateInitializing, "")

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.active, repDoc.ID)
			c.mu.Unlock()
		}()

		onState(model.ReplicationStateRunning, "")

		_, err := replicator.Run(repCtx)
		if err != nil {
			c.Logger.Errorf(repCtx, "replication failed", "docID", repDoc.ID, "repID", repID, "error", err)

			// Increment retry metadata AND set crashing/error state atomically
			// (combined to avoid race with change listener)
			c.incrementRetryCount(repCtx, repDoc, err.Error())
			return
		}

		// Success - reset retry count
		c.resetRetryCount(repCtx, repDoc)

		if !repDoc.Continuous {
			onState(model.ReplicationStateCompleted, "")
		}
	}()
}

// IsRunning reports whether a replication job with the given ID is currently active.
func (c *Replication) IsRunning(repID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.active[repID]
	return ok
}

// Cancel stops the replication job with the given ID, if any.
func (c *Replication) Cancel(repID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.active[repID]; ok {
		cancel()
		delete(c.active, repID)
	}
}

// CancelStaleJobs cancels any running jobs whose IDs are not in activeIDs.
// Used by the controller to stop jobs whose _replicator documents were deleted.
func (c *Replication) CancelStaleJobs(activeIDs map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, cancel := range c.active {
		if !activeIDs[id] {
			cancel()
			delete(c.active, id)
		}
	}
}

// CancelAll stops all active replication jobs.
func (c *Replication) CancelAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, cancel := range c.active {
		cancel()
		delete(c.active, id)
	}
}
