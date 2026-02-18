package controller

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const changesBatchSize = 100

// Replicator performs replication between a source and target ReplicationPeer
type Replicator struct {
	Source       port.ReplicationPeer
	Target       port.ReplicationPeer
	Logger       port.Logger
	Continuous   bool
	CreateTarget bool
	RepID        string // unique replication ID
}

// replicationID generates a deterministic replication ID from source+target
func replicationID(source, target string) string {
	h := md5.New()
	h.Write([]byte(source))
	h.Write([]byte(target))
	return hex.EncodeToString(h.Sum(nil))
}

// NewReplicator creates a new Replicator
func NewReplicator(source, target port.ReplicationPeer, repDoc *model.ReplicationDoc, logger port.Logger) *Replicator {
	repID := replicationID(repDoc.Source, repDoc.Target)
	return &Replicator{
		Source:       source,
		Target:       target,
		Logger:       logger.With("repID", repID),
		Continuous:   repDoc.Continuous,
		CreateTarget: repDoc.CreateTarget,
		RepID:        repID,
	}
}

// Run executes the replication. For one-shot it returns when complete.
// For continuous it runs until context is cancelled.
func (r *Replicator) Run(ctx context.Context) (*model.ReplicationResult, error) {
	r.Logger.Infof(ctx, "replication starting")
	result := &model.ReplicationResult{
		StartTime: time.Now(),
	}

	// 1. Verify peers
	if err := r.verifyPeers(ctx); err != nil {
		r.Logger.Errorf(ctx, "peer verification failed", "error", err)
		return result, err
	}
	r.Logger.Debugf(ctx, "peers verified successfully")

	// 2. Find checkpoint
	since := r.loadCheckpoint(ctx)
	if since != "" {
		r.Logger.Infof(ctx, "resuming from checkpoint", "since", since)
	}
	sessionID := fmt.Sprintf("%d", time.Now().UnixNano())

	// 3. Replicate
	for {
		select {
		case <-ctx.Done():
			result.EndTime = time.Now()
			r.Logger.Infof(ctx, "replication cancelled", "docs_read", result.DocsRead, "docs_written", result.DocsWritten)
			return result, nil
		default:
		}

		batchResult, newSince, pending, err := r.replicateBatch(ctx, since)
		if err != nil {
			r.Logger.Errorf(ctx, "batch replication failed", "error", err, "docs_read", result.DocsRead, "docs_written", result.DocsWritten)
			return result, err
		}

		result.DocsRead += batchResult.DocsRead
		result.DocsWritten += batchResult.DocsWritten

		if batchResult.DocsWritten > 0 {
			r.Logger.Infof(ctx, "batch completed", "docs_read", batchResult.DocsRead, "docs_written", batchResult.DocsWritten, "pending", pending)
		}

		if newSince != since && newSince != "" {
			since = newSince
			// Save checkpoint after each batch
			r.saveCheckpoint(ctx, since, sessionID, result)
			r.Logger.Debugf(ctx, "checkpoint saved", "since", since)
		}

		if !r.Continuous {
			if pending == 0 || len(batchResult.changes) == 0 {
				break
			}
			continue
		}

		// Continuous mode: if no changes, poll
		if len(batchResult.changes) == 0 || pending == 0 {
			select {
			case <-ctx.Done():
				result.EndTime = time.Now()
				return result, nil
			case <-time.After(time.Second):
				continue
			}
		}
	}

	result.EndTime = time.Now()
	r.Logger.Infof(ctx, "replication completed", "docs_read", result.DocsRead, "docs_written", result.DocsWritten, "duration", result.EndTime.Sub(result.StartTime))
	return result, nil
}

func (r *Replicator) verifyPeers(ctx context.Context) error {
	// Verify source
	r.Logger.Debugf(ctx, "verifying source database")
	if err := r.Source.Head(ctx); err != nil {
		r.Logger.Warnf(ctx, "source database verification failed", "error", err)
		return fmt.Errorf("source database not available: %w", err)
	}

	// Verify target
	r.Logger.Debugf(ctx, "verifying target database")
	err := r.Target.Head(ctx)
	if err != nil {
		if r.CreateTarget {
			r.Logger.Infof(ctx, "target database does not exist, creating it")
			if err := r.Target.CreateDB(ctx); err != nil {
				r.Logger.Errorf(ctx, "failed to create target database", "error", err)
				return fmt.Errorf("failed to create target database: %w", err)
			}
			r.Logger.Infof(ctx, "target database created successfully")
		} else {
			r.Logger.Warnf(ctx, "target database verification failed", "error", err)
			return fmt.Errorf("target database not available: %w", err)
		}
	}

	return nil
}

func (r *Replicator) checkpointDocID() string {
	return r.RepID
}

func (r *Replicator) loadCheckpoint(ctx context.Context) string {
	docID := r.checkpointDocID()

	// Try source checkpoint
	doc, err := r.Source.GetLocalDoc(ctx, docID)
	if err != nil || doc == nil {
		return ""
	}

	sourceSeq, _ := doc.Data["source_last_seq"].(string)
	if sourceSeq == "" {
		return ""
	}

	// Verify target has matching checkpoint
	tDoc, err := r.Target.GetLocalDoc(ctx, docID)
	if err != nil || tDoc == nil {
		return ""
	}

	targetSeq, _ := tDoc.Data["source_last_seq"].(string)
	sessionSource, _ := doc.Data["session_id"].(string)
	sessionTarget, _ := tDoc.Data["session_id"].(string)

	if sourceSeq == targetSeq && sessionSource == sessionTarget {
		return sourceSeq
	}

	return ""
}

func (r *Replicator) saveCheckpoint(ctx context.Context, since, sessionID string, result *model.ReplicationResult) {
	docID := r.checkpointDocID()

	data := map[string]interface{}{
		"source_last_seq": since,
		"session_id":      sessionID,
		"history": []map[string]interface{}{
			{
				"session_id":      sessionID,
				"source_last_seq": since,
				"docs_read":       result.DocsRead,
				"docs_written":    result.DocsWritten,
				"start_time":      result.StartTime.Format(time.RFC3339),
				"end_time":        time.Now().Format(time.RFC3339),
			},
		},
	}

	saveToOnePeer := func(peer port.ReplicationPeer) {
		fullID := string(model.LocalDocPrefix) + docID
		// Try to read existing doc to get rev
		existing, err := peer.GetLocalDoc(ctx, docID)
		// Make a copy of data for this peer
		peerData := make(map[string]interface{}, len(data))
		for k, v := range data {
			peerData[k] = v
		}
		doc := &model.Document{
			ID:   fullID,
			Data: peerData,
		}
		if err == nil && existing != nil {
			doc.Rev = existing.Rev
		}
		peerData["_id"] = doc.ID
		if doc.Rev != "" {
			peerData["_rev"] = doc.Rev
		}

		err = peer.PutLocalDoc(ctx, doc)
		if err != nil {
			r.Logger.Warnf(ctx, "checkpoint save failed", "error", err)
		}
	}

	saveToOnePeer(r.Source)
	saveToOnePeer(r.Target)
}

type batchResult struct {
	DocsRead    int
	DocsWritten int
	changes     []model.ChangeResult
}

func (r *Replicator) replicateBatch(ctx context.Context, since string) (*batchResult, string, int, error) {
	br := &batchResult{}

	// Get changes from source
	changesResp, err := r.Source.GetChanges(ctx, since, changesBatchSize)
	if err != nil {
		return br, since, 0, fmt.Errorf("failed to get changes: %w", err)
	}

	br.changes = changesResp.Results
	if len(changesResp.Results) == 0 {
		return br, changesResp.LastSeq, changesResp.Pending, nil
	}

	// Build revs map for RevsDiff
	revsMap := make(map[string][]string)
	for _, change := range changesResp.Results {
		if change.ID == "" {
			continue
		}
		// Skip local docs
		doc := model.Document{ID: change.ID}
		if doc.IsLocalDoc() {
			continue
		}
		for _, cr := range change.Changes {
			revsMap[change.ID] = append(revsMap[change.ID], cr.Rev)
		}
	}

	if len(revsMap) == 0 {
		return br, changesResp.LastSeq, changesResp.Pending, nil
	}

	// Find missing revisions on target
	missing, err := r.Target.RevsDiff(ctx, revsMap)
	if err != nil {
		return br, since, 0, fmt.Errorf("failed to get revs diff: %w", err)
	}

	if len(missing) == 0 {
		return br, changesResp.LastSeq, changesResp.Pending, nil
	}

	// Fetch missing docs from source
	var docs []*model.Document
	for docID, diff := range missing {
		select {
		case <-ctx.Done():
			return br, since, 0, ctx.Err()
		default:
		}

		doc, err := r.Source.GetDoc(ctx, docID, true, diff.Missing)
		if err != nil {
			r.Logger.Warnf(ctx, "failed to get doc", "docID", docID, "error", err)
			continue
		}
		br.DocsRead++
		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return br, changesResp.LastSeq, changesResp.Pending, nil
	}

	// Write docs to target
	err = r.Target.BulkDocs(ctx, docs, false)
	if err != nil {
		return br, since, 0, fmt.Errorf("failed to write docs to target: %w", err)
	}
	br.DocsWritten = len(docs)

	return br, changesResp.LastSeq, changesResp.Pending, nil
}
