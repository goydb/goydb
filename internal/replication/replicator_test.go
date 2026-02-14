package replication

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockPeer implements Peer backed by in-memory maps for testing
type MockPeer struct {
	mu       sync.Mutex
	docs     map[string]*model.Document
	localDocs map[string]*model.Document
	exists   bool
	seq      int

	HeadCalls     int
	CreateDBCalls int
}

func NewMockPeer(exists bool) *MockPeer {
	return &MockPeer{
		docs:      make(map[string]*model.Document),
		localDocs: make(map[string]*model.Document),
		exists:    exists,
	}
}

func (m *MockPeer) Head(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.HeadCalls++
	if !m.exists {
		return fmt.Errorf("database not found")
	}
	return nil
}

func (m *MockPeer) GetDBInfo(ctx context.Context) (*DBInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &DBInfo{
		DBName:    "mockdb",
		UpdateSeq: strconv.Itoa(m.seq),
	}, nil
}

func (m *MockPeer) GetLocalDoc(ctx context.Context, docID string) (*model.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.localDocs[docID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return doc, nil
}

func (m *MockPeer) PutLocalDoc(ctx context.Context, doc *model.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := doc.ID
	if len(key) > 7 && key[:7] == "_local/" {
		key = key[7:]
	}
	m.localDocs[key] = doc
	return nil
}

func (m *MockPeer) AddDoc(id, rev string, deleted bool, data map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	if data == nil {
		data = make(map[string]interface{})
	}
	data["_id"] = id
	data["_rev"] = rev
	m.docs[id] = &model.Document{
		ID:       id,
		Rev:      rev,
		Deleted:  deleted,
		LocalSeq: uint64(m.seq),
		Data:     data,
	}
}

func (m *MockPeer) GetChanges(ctx context.Context, since string, limit int) (*ChangesResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sinceSeq := 0
	if since != "" {
		sinceSeq, _ = strconv.Atoi(since)
	}

	var results []ChangeResult
	maxSeq := 0
	for _, doc := range m.docs {
		if int(doc.LocalSeq) > sinceSeq {
			results = append(results, ChangeResult{
				Seq:     strconv.FormatUint(doc.LocalSeq, 10),
				ID:      doc.ID,
				Deleted: doc.Deleted,
				Changes: []ChangeRev{{Rev: doc.Rev}},
				Doc:     doc,
			})
		}
		if int(doc.LocalSeq) > maxSeq {
			maxSeq = int(doc.LocalSeq)
		}
	}

	// Apply limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	lastSeq := since
	if len(results) > 0 {
		lastSeq = results[len(results)-1].Seq
	}

	pending := 0
	if limit > 0 && len(results) >= limit {
		total := 0
		for _, doc := range m.docs {
			if int(doc.LocalSeq) > sinceSeq {
				total++
			}
		}
		pending = total - len(results)
	}

	return &ChangesResponse{
		Results: results,
		LastSeq: lastSeq,
		Pending: pending,
	}, nil
}

func (m *MockPeer) RevsDiff(ctx context.Context, revs map[string][]string) (map[string]*RevsDiffResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]*RevsDiffResult)
	for docID, docRevs := range revs {
		doc, ok := m.docs[docID]
		if !ok {
			result[docID] = &RevsDiffResult{Missing: docRevs}
			continue
		}
		var missing []string
		for _, rev := range docRevs {
			if rev != doc.Rev {
				missing = append(missing, rev)
			}
		}
		if len(missing) > 0 {
			result[docID] = &RevsDiffResult{Missing: missing}
		}
	}
	return result, nil
}

func (m *MockPeer) GetDoc(ctx context.Context, docID string, revs bool, openRevs []string) (*model.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.docs[docID]
	if !ok {
		return nil, fmt.Errorf("document %q not found", docID)
	}

	result := &model.Document{
		ID:      doc.ID,
		Rev:     doc.Rev,
		Deleted: doc.Deleted,
		Data:    copyMap(doc.Data),
	}
	if revs {
		result.Data["_revisions"] = result.Revisions()
	}
	return result, nil
}

func (m *MockPeer) BulkDocs(ctx context.Context, docs []*model.Document, newEdits bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, doc := range docs {
		m.seq++
		stored := &model.Document{
			ID:       doc.ID,
			Rev:      doc.Rev,
			Deleted:  doc.Deleted,
			LocalSeq: uint64(m.seq),
			Data:     copyMap(doc.Data),
		}
		m.docs[doc.ID] = stored
	}
	return nil
}

func (m *MockPeer) CreateDB(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateDBCalls++
	m.exists = true
	return nil
}

func (m *MockPeer) DocCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.docs)
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return make(map[string]interface{})
	}
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func TestReplicatorOneShot_EmptyTarget(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 5; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, map[string]interface{}{"value": i})
	}

	repDoc := &model.ReplicationDoc{
		Source: "source",
		Target: "target",
	}

	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 5, result.DocsWritten)
	assert.Equal(t, 5, target.DocCount())
}

func TestReplicatorOneShot_PartialOverlap(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 10; i++ {
		rev := fmt.Sprintf("1-%032d", i)
		source.AddDoc(fmt.Sprintf("doc%d", i), rev, false, map[string]interface{}{"value": i})
	}
	// Target already has 4 docs
	for i := 0; i < 4; i++ {
		rev := fmt.Sprintf("1-%032d", i)
		target.AddDoc(fmt.Sprintf("doc%d", i), rev, false, map[string]interface{}{"value": i})
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 6, result.DocsWritten)
	assert.Equal(t, 10, target.DocCount())
}

func TestReplicatorOneShot_CreateTarget(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(false) // doesn't exist

	source.AddDoc("doc1", "1-abc", false, nil)

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target", CreateTarget: true}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.DocsWritten)
	assert.Equal(t, 1, target.CreateDBCalls)
}

func TestReplicatorOneShot_TargetNotFound(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(false)

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target", CreateTarget: false}
	r := NewReplicator(source, target, repDoc)
	_, err := r.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target database not available")
}

func TestReplicatorOneShot_EmptySource(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.DocsWritten)
}

func TestReplicatorOneShot_DeletedDocs(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	source.AddDoc("doc1", "1-aaa", false, nil)
	source.AddDoc("doc2", "1-bbb", false, nil)
	source.AddDoc("doc3", "2-ccc", true, nil) // deleted

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	result, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, result.DocsWritten)
	assert.Equal(t, 3, target.DocCount())
}

func TestReplicatorContinuous_NewDocsArrive(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 3; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target", Continuous: true}
	r := NewReplicator(source, target, repDoc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(200 * time.Millisecond)
		source.AddDoc("doc3", "1-00000000000000000000000000000003", false, nil)
		source.AddDoc("doc4", "1-00000000000000000000000000000004", false, nil)
	}()

	go func() {
		// Wait for all 5 docs to arrive, then cancel
		for {
			if target.DocCount() >= 5 {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	result, _ := r.Run(ctx)
	assert.GreaterOrEqual(t, result.DocsWritten, 5)
	assert.Equal(t, 5, target.DocCount())
}

func TestReplicatorContinuous_CancelStopsCleanly(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target", Continuous: true}
	r := NewReplicator(source, target, repDoc)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.Run(ctx) // nolint: errcheck
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// ok
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestReplicatorCheckpointFormat(t *testing.T) {
	source := NewMockPeer(true)
	target := NewMockPeer(true)

	for i := 0; i < 5; i++ {
		source.AddDoc(fmt.Sprintf("doc%d", i), fmt.Sprintf("1-%032d", i), false, nil)
	}

	repDoc := &model.ReplicationDoc{Source: "source", Target: "target"}
	r := NewReplicator(source, target, repDoc)
	_, err := r.Run(context.Background())
	require.NoError(t, err)

	// Check checkpoint exists on source
	cpID := r.RepID
	sDoc, err := source.GetLocalDoc(context.Background(), cpID)
	require.NoError(t, err)
	assert.NotNil(t, sDoc)
	assert.NotEmpty(t, sDoc.Data["source_last_seq"])
	assert.NotEmpty(t, sDoc.Data["session_id"])

	// Check checkpoint exists on target
	tDoc, err := target.GetLocalDoc(context.Background(), cpID)
	require.NoError(t, err)
	assert.NotNil(t, tDoc)
	assert.Equal(t, sDoc.Data["source_last_seq"], tDoc.Data["source_last_seq"])
}
