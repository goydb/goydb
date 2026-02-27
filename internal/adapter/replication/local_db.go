package replication

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// LocalDB adapts a port.Storage/port.Database to the port.ReplicationPeer interface.
// It lives here (adapter layer) rather than in the protocol package so that
// internal/replication stays free of storage adapter imports.
type LocalDB struct {
	Storage port.Storage
	DBName  string
}

var _ port.ReplicationPeer = (*LocalDB)(nil)

func (l *LocalDB) db(ctx context.Context) (port.Database, error) {
	return l.Storage.Database(ctx, l.DBName)
}

func (l *LocalDB) Head(ctx context.Context) error {
	_, err := l.db(ctx)
	return err
}

func (l *LocalDB) GetDBInfo(ctx context.Context) (*model.DBInfo, error) {
	db, err := l.db(ctx)
	if err != nil {
		return nil, err
	}

	seq, err := db.Sequence(ctx)
	if err != nil {
		return nil, err
	}

	return &model.DBInfo{
		DBName:    l.DBName,
		UpdateSeq: seq,
	}, nil
}

func (l *LocalDB) GetLocalDoc(ctx context.Context, docID string) (*model.Document, error) {
	db, err := l.db(ctx)
	if err != nil {
		return nil, err
	}

	fullID := string(model.LocalDocPrefix) + docID
	doc, err := db.GetDocument(ctx, fullID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("local doc %q not found", docID)
	}
	return doc, nil
}

func (l *LocalDB) PutLocalDoc(ctx context.Context, doc *model.Document) error {
	db, err := l.db(ctx)
	if err != nil {
		return err
	}

	_, err = db.PutDocument(ctx, doc)
	return err
}

func (l *LocalDB) GetChanges(ctx context.Context, since string, limit int) (*model.ChangesResponse, error) {
	db, err := l.db(ctx)
	if err != nil {
		return nil, err
	}

	// Use db.Changes() which iterates the changes index in sequence order.
	// This is critical: iterating the docs bucket (alphabetical by docID) causes
	// documents to be skipped when batching because LastSeq from an alphabetically
	// ordered batch doesn't represent a contiguous sequence boundary.
	//
	// The changes index stores entries at key=seq, but doc.LocalSeq (from the
	// invalidation index) is seq-1.  When a previous response reported
	// LastSeq=N (based on LocalSeq), Changes() would Seek(N)+Next(), landing
	// on the entry whose doc has LocalSeq=N — the same doc already processed.
	// Increment since by 1 so Changes() correctly starts AFTER that entry.
	//
	// Normalize since="0" or "" to "" so db.Changes() starts from the very
	// first entry (cursor.First()).
	if since == "0" || since == "" {
		since = ""
	} else {
		sinceVal, err := strconv.ParseUint(since, 10, 64)
		if err == nil {
			since = strconv.FormatUint(sinceVal+1, 10)
		}
	}
	opts := &model.ChangesOptions{
		Since:   since,
		Limit:   limit,
		Timeout: time.Millisecond,
	}
	docs, pending, err := db.Changes(ctx, opts)
	if err != nil {
		return nil, err
	}

	resp := &model.ChangesResponse{
		Pending: pending,
	}

	var lastSeq uint64
	for _, doc := range docs {
		cr := model.ChangeResult{
			Seq:     strconv.FormatUint(doc.LocalSeq, 10),
			ID:      doc.ID,
			Deleted: doc.Deleted,
			Changes: []model.ChangeRev{{Rev: doc.Rev}},
			Doc:     doc,
		}
		resp.Results = append(resp.Results, cr)

		if doc.LocalSeq > lastSeq {
			lastSeq = doc.LocalSeq
		}
	}

	if lastSeq > 0 {
		resp.LastSeq = strconv.FormatUint(lastSeq, 10)
	} else {
		resp.LastSeq = since
	}

	return resp, nil
}

func (l *LocalDB) RevsDiff(ctx context.Context, revs map[string][]string) (map[string]*model.RevsDiffResult, error) {
	db, err := l.db(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*model.RevsDiffResult)
	for docID, docRevs := range revs {
		doc, err := db.GetDocument(ctx, docID)
		if err != nil || doc == nil {
			result[docID] = &model.RevsDiffResult{Missing: docRevs}
			continue
		}

		var missing []string
		for _, rev := range docRevs {
			if !doc.HasRevision(rev) {
				missing = append(missing, rev)
			}
		}
		if len(missing) > 0 {
			result[docID] = &model.RevsDiffResult{Missing: missing}
		}
	}

	return result, nil
}

func (l *LocalDB) GetDoc(ctx context.Context, docID string, revs bool, openRevs []string) (*model.Document, error) {
	db, err := l.db(ctx)
	if err != nil {
		return nil, err
	}

	doc, err := db.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("document %q not found", docID)
	}

	if revs {
		doc.Data["_revisions"] = doc.Revisions()
	}

	// Inline attachment blobs as base64 so the replicator can carry the data
	// to the target without a separate request (CouchDB inline attachment format).
	for name, att := range doc.Attachments {
		if att == nil || att.Digest == "" {
			continue
		}
		reader, err := db.AttachmentReader(att.Digest)
		if err != nil {
			continue // blob missing; skip rather than failing the whole doc
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			continue
		}
		attCopy := *att
		attCopy.Stub = false
		attCopy.Data = base64.StdEncoding.EncodeToString(data)
		doc.Attachments[name] = &attCopy
	}

	return doc, nil
}

// BulkGet fetches multiple documents from local storage by delegating to GetDoc per request.
// GetDoc already inlines attachment blobs as base64, so no further handling is needed.
func (l *LocalDB) BulkGet(ctx context.Context, docs []port.BulkGetRequest) ([]*model.Document, error) {
	var result []*model.Document
	for _, req := range docs {
		doc, err := l.GetDoc(ctx, req.ID, true, req.Revs)
		if err != nil {
			continue // skip missing docs, same as replicator's GetDoc loop
		}
		result = append(result, doc)
	}
	return result, nil
}

// EnsureFullCommit is a no-op for local storage: writes are durable
// immediately after the bbolt transaction commits.
func (l *LocalDB) EnsureFullCommit(ctx context.Context) error {
	return nil
}

func (l *LocalDB) BulkDocs(ctx context.Context, docs []*model.Document, newEdits bool) error {
	db, err := l.db(ctx)
	if err != nil {
		return err
	}

	return db.Transaction(ctx, func(tx port.DatabaseTx) error {
		for _, doc := range docs {
			if !newEdits {
				err := tx.PutDocumentForReplication(ctx, doc)
				if err != nil {
					return err
				}
			} else {
				_, err := tx.PutDocument(ctx, doc)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (l *LocalDB) CreateDB(ctx context.Context) error {
	_, err := l.Storage.CreateDatabase(ctx, l.DBName)
	return err
}
