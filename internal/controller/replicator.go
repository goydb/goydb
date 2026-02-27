package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
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

// writeEndpoint writes a URL and its sorted key=value header pairs into h,
// mirroring the reference replicator's endpoint contribution to the rep ID.
func writeEndpoint(h io.Writer, rawURL string, headers map[string]string) {
	io.WriteString(h, rawURL) //nolint:errcheck
	io.WriteString(h, "|")   //nolint:errcheck
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		io.WriteString(h, k)          //nolint:errcheck
		io.WriteString(h, "|")        //nolint:errcheck
		io.WriteString(h, headers[k]) //nolint:errcheck
		io.WriteString(h, "|")        //nolint:errcheck
	}
}

// replicationID generates a deterministic replication ID from source, target,
// their custom headers, and job flags so that one-shot and continuous jobs to
// the same endpoints produce distinct checkpoint documents, and jobs with
// different auth headers do not share checkpoints.
func replicationID(source string, sourceHeaders map[string]string,
	target string, targetHeaders map[string]string,
	continuous, createTarget bool) string {
	h := sha256.New()
	writeEndpoint(h, source, sourceHeaders)
	io.WriteString(h, "|") //nolint:errcheck
	writeEndpoint(h, target, targetHeaders)
	io.WriteString(h, "|") //nolint:errcheck
	if createTarget {
		io.WriteString(h, "T") //nolint:errcheck
	} else {
		io.WriteString(h, "F") //nolint:errcheck
	}
	if continuous {
		io.WriteString(h, "T") //nolint:errcheck
	} else {
		io.WriteString(h, "F") //nolint:errcheck
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NewReplicator creates a new Replicator
func NewReplicator(source, target port.ReplicationPeer, repDoc *model.ReplicationDoc, logger port.Logger) *Replicator {
	repID := replicationID(
		repDoc.Source, repDoc.SourceHeaders,
		repDoc.Target, repDoc.TargetHeaders,
		repDoc.Continuous, repDoc.CreateTarget,
	)
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

		startSince := since
		batchResult, newSince, pending, err := r.replicateBatch(ctx, since)
		if err != nil {
			r.Logger.Errorf(ctx, "batch replication failed", "error", err, "docs_read", result.DocsRead, "docs_written", result.DocsWritten)
			return result, err
		}

		result.DocsRead += batchResult.DocsRead
		result.DocsWritten += batchResult.DocsWritten
		result.MissingFound += batchResult.MissingFound
		result.MissingChecked += batchResult.MissingChecked

		if batchResult.DocsWritten > 0 {
			r.Logger.Infof(ctx, "batch completed", "docs_read", batchResult.DocsRead, "docs_written", batchResult.DocsWritten, "pending", pending)
		}

		if newSince != since && newSince != "" {
			since = newSince
			// Save checkpoint after each batch
			r.saveCheckpoint(ctx, since, startSince, sessionID, result, batchResult)
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

// unmarshalHistory extracts the history array from a checkpoint document's Data map.
func unmarshalHistory(data map[string]interface{}) []model.ReplicationCheckpointHist {
	raw, ok := data["history"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var h []model.ReplicationCheckpointHist
	_ = json.Unmarshal(b, &h)
	return h
}

func (r *Replicator) loadCheckpoint(ctx context.Context) string {
	docID := r.checkpointDocID()

	sDoc, err := r.Source.GetLocalDoc(ctx, docID)
	if err != nil || sDoc == nil {
		return ""
	}
	tDoc, err := r.Target.GetLocalDoc(ctx, docID)
	if err != nil || tDoc == nil {
		return ""
	}

	srcSeq, _ := sDoc.Data["source_last_seq"].(string)
	srcSID, _ := sDoc.Data["session_id"].(string)
	tgtSID, _ := tDoc.Data["session_id"].(string)

	// Fast path: top-level session IDs agree.
	if srcSID == tgtSID && srcSeq != "" {
		return srcSeq
	}

	// Slow path: search history arrays for a common session_id.
	srcHist := unmarshalHistory(sDoc.Data)
	tgtHist := unmarshalHistory(tDoc.Data)
	for _, sl := range srcHist {
		for _, tl := range tgtHist {
			if sl.SessionID == tl.SessionID && sl.RecordedSeq != "" {
				return sl.RecordedSeq
			}
		}
	}

	return "" // no common ancestry; full replication
}

func (r *Replicator) saveCheckpoint(ctx context.Context, since, startSince, sessionID string, result *model.ReplicationResult, batch *batchResult) {
	docID := r.checkpointDocID()
	now := time.Now()

	newEntry := model.ReplicationCheckpointHist{
		SessionID:        sessionID,
		StartLastSeq:     startSince,
		EndLastSeq:       since,
		RecordedSeq:      since,
		SourceLastSeq:    since,
		DocsRead:         result.DocsRead,
		DocsWritten:      result.DocsWritten,
		DocWriteFailures: result.DocWriteFailures,
		MissingFound:     result.MissingFound,
		MissingChecked:   result.MissingChecked,
		StartTime:        result.StartTime.Format(time.RFC3339),
		EndTime:          now.Format(time.RFC3339),
	}

	saveToOnePeer := func(peer port.ReplicationPeer) {
		fullID := string(model.LocalDocPrefix) + docID
		existing, _ := peer.GetLocalDoc(ctx, docID)

		cp := model.ReplicationCheckpoint{
			ReplicationIDVersion: 3,
			SessionID:            sessionID,
			SourceLastSeq:        since,
		}
		// Preserve existing history and append new entry.
		if existing != nil {
			if raw, ok := existing.Data["history"]; ok {
				if b, err := json.Marshal(raw); err == nil {
					_ = json.Unmarshal(b, &cp.History)
				}
			}
		}
		cp.History = append(cp.History, newEntry)

		cpData, _ := json.Marshal(cp)
		var m map[string]interface{}
		_ = json.Unmarshal(cpData, &m)
		m["_id"] = fullID
		if existing != nil && existing.Rev != "" {
			m["_rev"] = existing.Rev
		}
		doc := &model.Document{ID: fullID, Data: m}
		if existing != nil {
			doc.Rev = existing.Rev
		}
		if err := peer.PutLocalDoc(ctx, doc); err != nil {
			r.Logger.Warnf(ctx, "checkpoint save failed", "error", err)
		}
	}

	saveToOnePeer(r.Source)
	saveToOnePeer(r.Target)
}

type batchResult struct {
	DocsRead         int
	DocsWritten      int
	DocWriteFailures int
	MissingFound     int
	MissingChecked   int
	changes          []model.ChangeResult
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
	br.MissingFound = len(revsMap)

	// Find missing revisions on target
	missing, err := r.Target.RevsDiff(ctx, revsMap)
	if err != nil {
		return br, since, 0, fmt.Errorf("failed to get revs diff: %w", err)
	}
	br.MissingChecked = len(missing)

	if len(missing) == 0 {
		return br, changesResp.LastSeq, changesResp.Pending, nil
	}

	// Fetch missing docs from source in a single batch request
	var requests []port.BulkGetRequest
	for docID, diff := range missing {
		requests = append(requests, port.BulkGetRequest{ID: docID, Revs: diff.Missing})
	}
	docs, err := r.Source.BulkGet(ctx, requests)
	if err != nil {
		return br, since, 0, fmt.Errorf("failed to bulk-get docs from source: %w", err)
	}
	br.DocsRead = len(docs)

	if len(docs) == 0 {
		return br, changesResp.LastSeq, changesResp.Pending, nil
	}

	// Write docs to target
	err = r.Target.BulkDocs(ctx, docs, false)
	if err != nil {
		return br, since, 0, fmt.Errorf("failed to write docs to target: %w", err)
	}
	br.DocsWritten = len(docs)

	// Ensure writes are durable (CouchDB protocol step 5).
	if err := r.Target.EnsureFullCommit(ctx); err != nil {
		r.Logger.Warnf(ctx, "ensure_full_commit failed", "error", err)
	}

	return br, changesResp.LastSeq, changesResp.Pending, nil
}
