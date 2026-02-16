package controller

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	adapterreplication "github.com/goydb/goydb/internal/adapter/replication"
	"github.com/goydb/goydb/internal/replication"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// ReplicationService owns peer construction and goroutine lifecycle for
// replication jobs. It is shared between the Replication controller (async,
// _replicator-DB jobs) and the POST /_replicate HTTP handler (synchronous).
type ReplicationService struct {
	Storage port.Storage

	mu     sync.Mutex
	active map[string]context.CancelFunc
}

// BuildPeer is the single source of truth for creating a replication.Peer
// from an address string. HTTP(S) addresses become remote clients; everything
// else is treated as a local database name.
func (s *ReplicationService) BuildPeer(addr string) replication.Peer {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		client, err := replication.NewClient(addr)
		if err != nil {
			log.Printf("Failed to create remote peer for %s: %v", addr, err)
			return nil
		}
		return client
	}
	return &adapterreplication.LocalDB{
		Storage: s.Storage,
		DBName:  addr,
	}
}

// Run executes a replication synchronously and returns the result. Used by the
// POST /_replicate HTTP handler.
func (s *ReplicationService) Run(ctx context.Context, repDoc *model.ReplicationDoc) (*replication.ReplicationResult, error) {
	source := s.BuildPeer(repDoc.Source)
	target := s.BuildPeer(repDoc.Target)
	if source == nil || target == nil {
		return nil, fmt.Errorf("invalid source or target")
	}
	return replication.NewReplicator(source, target, repDoc).Run(ctx)
}

// Submit starts an async replication goroutine for a _replicator-DB job.
// onState is called on each state transition so the caller can persist the
// state back to the document.
func (s *ReplicationService) Submit(ctx context.Context, repDoc *model.ReplicationDoc, onState func(model.ReplicationState, string)) {
	source := s.BuildPeer(repDoc.Source)
	target := s.BuildPeer(repDoc.Target)
	if source == nil || target == nil {
		onState(model.ReplicationStateError, "invalid source or target")
		return
	}

	replicator := replication.NewReplicator(source, target, repDoc)
	repCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	if s.active == nil {
		s.active = make(map[string]context.CancelFunc)
	}
	s.active[repDoc.ID] = cancel
	s.mu.Unlock()

	onState(model.ReplicationStateInitializing, "")

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.active, repDoc.ID)
			s.mu.Unlock()
		}()

		onState(model.ReplicationStateRunning, "")

		_, err := replicator.Run(repCtx)
		if err != nil {
			log.Printf("Replication %s failed: %v", repDoc.ID, err)
			onState(model.ReplicationStateError, err.Error())
			return
		}

		if !repDoc.Continuous {
			onState(model.ReplicationStateCompleted, "")
		}
	}()
}

// IsRunning reports whether a replication job with the given ID is currently active.
func (s *ReplicationService) IsRunning(repID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.active[repID]
	return ok
}

// Cancel stops the replication job with the given ID, if any.
func (s *ReplicationService) Cancel(repID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.active[repID]; ok {
		cancel()
		delete(s.active, repID)
	}
}

// CancelStaleJobs cancels any running jobs whose IDs are not in activeIDs.
// Used by the controller to stop jobs whose _replicator documents were deleted.
func (s *ReplicationService) CancelStaleJobs(activeIDs map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.active {
		if !activeIDs[id] {
			cancel()
			delete(s.active, id)
		}
	}
}

// CancelAll stops all active replication jobs.
func (s *ReplicationService) CancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.active {
		cancel()
		delete(s.active, id)
	}
}
