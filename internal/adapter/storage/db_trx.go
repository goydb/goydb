package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"

	"github.com/fxamacker/cbor/v2"
	"github.com/goydb/goydb/internal/adapter/index"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

var ErrNotFound = errors.New("resource not found")
var ErrConflict = fmt.Errorf("rev doesn't match for update: %w", port.ErrConflict)
var ErrUnknownDatabase = errors.New("unknown database")

type Transaction struct {
	Database   *Database
	BucketName []byte
	port.EngineWriteTransaction
}

func (tx *Transaction) SetBucketName(bucketName []byte) {
	tx.BucketName = bucketName
}

func (tx *Transaction) bucket() []byte {
	if tx.BucketName != nil {
		return tx.BucketName
	} else {
		return model.DocsBucket
	}
}

func (tx *Transaction) GetRaw(ctx context.Context, key []byte, value interface{}) error {
	data, err := tx.Get(tx.bucket(), key)
	if err != nil {
		return err
	}

	err = bson.Unmarshal(data, value)
	if err != nil {
		return err
	}

	return nil
}

func (tx *Transaction) PutRaw(ctx context.Context, key []byte, raw interface{}) error {
	data, err := bson.Marshal(raw)
	if err != nil {
		return err
	}
	tx.Put(tx.bucket(), key, data)
	return nil
}

// leafKey encodes the composite key for a doc_leaves entry.
func leafKey(docID, rev string) []byte {
	k := make([]byte, len(docID)+1+len(rev))
	copy(k, docID)
	k[len(docID)] = 0
	copy(k[len(docID)+1:], rev)
	return k
}

// putLeaf serialises doc into the doc_leaves bucket.
func (tx *Transaction) putLeaf(doc *model.Document) error {
	data, err := bson.Marshal(doc)
	if err != nil {
		return err
	}
	tx.Put(model.DocLeavesBucket, leafKey(doc.ID, doc.Rev), data)
	return nil
}

// deleteLeaf removes a specific leaf entry (no-op if not present).
func (tx *Transaction) deleteLeaf(docID, rev string) {
	tx.Delete(model.DocLeavesBucket, leafKey(docID, rev))
}

// leafRevs returns all leaf revision strings for docID (prefix scan).
func (tx *Transaction) leafRevs(docID string) []string {
	prefix := append([]byte(docID), 0)
	cursor := tx.Cursor(model.DocLeavesBucket)
	var revs []string
	for k, _ := cursor.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cursor.Next() {
		revs = append(revs, string(k[len(prefix):]))
	}
	return revs
}

// getLeaf deserialises a single leaf from the doc_leaves bucket.
func (tx *Transaction) getLeaf(docID, rev string) (*model.Document, error) {
	data, err := tx.Get(model.DocLeavesBucket, leafKey(docID, rev))
	if err != nil {
		return nil, err
	}
	var doc model.Document
	if err := bson.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (tx *Transaction) PutDocument(ctx context.Context, doc *model.Document) (rev string, err error) {
	// verify that the transaction is valid for update
	oldDoc, err := tx.GetDocument(ctx, doc.ID)
	if err == nil && oldDoc != nil { // find if there is already a document
		if !oldDoc.ValidUpdateRevision(doc) {
			return "", ErrConflict
		}
	}

	// find next sequences (rev, changes)
	revSeq := doc.NextSequenceRevision()

	hash := md5.New()
	err = cbor.NewEncoder(hash).Encode(doc)
	if err != nil {
		return
	}
	rev = strconv.Itoa(revSeq) + "-" + hex.EncodeToString(hash.Sum(nil))
	doc.Rev = rev

	// Build revision history: prepend new rev, cap at 1000.
	oldHistory := []string{}
	if oldDoc != nil {
		if len(oldDoc.RevHistory) > 0 {
			oldHistory = oldDoc.RevHistory
		} else if oldDoc.Rev != "" {
			// Preserve the old rev for docs stored before RevHistory was introduced.
			oldHistory = []string{oldDoc.Rev}
		}
	}
	doc.RevHistory = append([]string{rev}, oldHistory...)
	if len(doc.RevHistory) > 1000 {
		doc.RevHistory = doc.RevHistory[:1000]
	}

	if oldDoc != nil {
		// maintain indices - remove old value
		for _, index := range tx.Database.Indices() {
			err := index.DocumentDeleted(ctx, tx, oldDoc)
			if err != nil {
				return "", err
			}
		}
	}

	err = tx.PutRaw(ctx, []byte(doc.ID), doc)
	if err != nil {
		return
	}

	// Maintain doc_leaves: remove old winner leaf, insert new winner leaf.
	if oldDoc != nil {
		tx.deleteLeaf(doc.ID, oldDoc.Rev)
	}
	_ = tx.putLeaf(doc) // non-critical; leaf is best-effort

	if doc.IsDesignDoc() {
		err = tx.Database.BuildDesignDocIndices(ctx, tx, doc, true)
		if err != nil {
			return
		}
	}

	// maintain Indices - add new value
	for _, index := range tx.Database.Indices() {
		err = index.DocumentStored(ctx, tx, doc)
		if err != nil {
			return
		}
	}

	return
}

func (tx *Transaction) GetDocument(ctx context.Context, docID string) (*model.Document, error) {
	var doc model.Document

	err := tx.GetRaw(ctx, []byte(docID), &doc)
	if err == port.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if doc.Data == nil {
		doc.Data = make(map[string]interface{})
	}
	doc.Data["_id"] = doc.ID
	doc.Data["_rev"] = doc.Rev
	if doc.Deleted {
		doc.Data["_deleted"] = true
	}
	if len(doc.Attachments) > 0 {
		doc.Data["_attachments"] = doc.Attachments
	}
	err = index.LocalSeq(ctx, tx.EngineWriteTransaction, &doc)
	if err != nil {
		return nil, err
	}

	// Populate _conflicts and augment RevHistory from conflict branches.
	leafRevs := tx.leafRevs(doc.ID)
	if slices.Contains(leafRevs, doc.Rev) && len(leafRevs) > 1 {
		knownRevs := make(map[string]bool, len(doc.RevHistory))
		for _, r := range doc.RevHistory {
			knownRevs[r] = true
		}

		var conflicts []string
		for _, leafRev := range leafRevs {
			if leafRev == doc.Rev {
				continue
			}
			conflicts = append(conflicts, leafRev)
			if leaf, err := tx.getLeaf(doc.ID, leafRev); err == nil {
				for _, r := range leaf.RevHistory {
					if !knownRevs[r] {
						knownRevs[r] = true
						doc.RevHistory = append(doc.RevHistory, r)
					}
				}
			}
		}
		doc.Data["_conflicts"] = conflicts
	}

	return &doc, nil
}

// PutDocumentForReplication stores a document preserving its existing revision.
// It skips conflict checks and does not generate a new revision, which is needed
// when replicating with new_edits=false.  Concurrent leaf revisions (conflicts)
// are tracked in the doc_leaves bucket; the CouchDB winning revision rule is
// applied to decide which revision is stored in the authoritative docs bucket.
func (tx *Transaction) PutDocumentForReplication(ctx context.Context, doc *model.Document) error {
	oldDoc, err := tx.GetDocument(ctx, doc.ID)
	if err == nil && oldDoc != nil {
		oldRev, _ := oldDoc.Revision()
		newRev, _ := doc.Revision()
		if oldRev == newRev {
			return nil // already have this revision
		}
	}

	// Build RevHistory from _revisions if the peer supplied it.
	if revsData, ok := doc.Data["_revisions"]; ok {
		if revsMap, ok := revsData.(map[string]interface{}); ok {
			start, _ := revsMap["start"].(float64)
			ids, _ := revsMap["ids"].([]interface{})
			history := make([]string, 0, len(ids))
			for i, id := range ids {
				if idStr, ok := id.(string); ok {
					seq := int64(start) - int64(i)
					history = append(history, fmt.Sprintf("%d-%s", seq, idStr))
				}
			}
			if len(history) > 1000 {
				history = history[:1000]
			}
			doc.RevHistory = history
		}
	}
	// Fallback: at minimum record the current rev so future PutDocument calls
	// can chain from it.
	if len(doc.RevHistory) == 0 && doc.Rev != "" {
		doc.RevHistory = []string{doc.Rev}
	}

	// Handle inline attachment data (base64-encoded) carried by the replicator.
	// Decode the data, write the blob to disk, update ref-counts, and replace
	// the inline entry with a proper stub before storing the document.
	for name, att := range doc.Attachments {
		if att == nil || att.Data == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			return fmt.Errorf("invalid base64 attachment data for %q: %w", name, err)
		}
		if att.Encoding == "gzip" {
			gr, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				return fmt.Errorf("gzip open for attachment %q: %w", name, err)
			}
			raw, err = io.ReadAll(gr)
			_ = gr.Close()
			if err != nil {
				return fmt.Errorf("gzip decompress attachment %q: %w", name, err)
			}
			att.Encoding = "" // store uncompressed; goydb has no gzip storage layer
		}
		// Compute content-addressed digest.
		sum := md5.New()
		sum.Write(raw)
		digest := hex.EncodeToString(sum.Sum(nil))
		// Write blob to the content-addressed filesystem location.
		if err := os.MkdirAll(tx.Database.blobDir(digest), 0755); err != nil {
			return fmt.Errorf("mkdir for attachment %q: %w", name, err)
		}
		blobDest := tx.Database.blobPath(digest)
		if _, statErr := os.Stat(blobDest); os.IsNotExist(statErr) {
			if err := os.WriteFile(blobDest, raw, 0644); err != nil {
				return fmt.Errorf("write attachment blob %q: %w", name, err)
			}
		}
		// Track the reference count within the transaction.
		if err := incAttRef(tx, digest); err != nil {
			return err
		}
		// Replace inline entry with a stub.
		att.Data = ""
		att.Digest = digest
		att.Length = int64(len(raw))
		att.Stub = true
	}

	// Build the leaf set in memory.
	// We read the committed state from the database and apply pending changes
	// manually, because the bbolt write transaction defers all writes to a
	// separate commit phase — writes are NOT visible to reads within the
	// same transaction.
	committedRevs := tx.leafRevs(doc.ID)
	leafSet := make(map[string]bool, len(committedRevs)+2)
	for _, r := range committedRevs {
		leafSet[r] = true
	}

	// If the existing doc predates doc_leaves, retroactively add it as a leaf.
	if oldDoc != nil && !leafSet[oldDoc.Rev] {
		leafSet[oldDoc.Rev] = true
		_ = tx.putLeaf(oldDoc)
	}

	// Add incoming doc as a new leaf.
	leafSet[doc.Rev] = true
	_ = tx.putLeaf(doc)

	// Remove any ancestor revisions that were previously leaves
	// (they now have a child, so they're no longer leaves).
	for _, ancestorRev := range doc.RevHistory[1:] {
		if leafSet[ancestorRev] {
			delete(leafSet, ancestorRev)
			tx.deleteLeaf(doc.ID, ancestorRev)
		}
	}

	// Recompute the winning revision across all known leaves (in-memory).
	allRevs := make([]string, 0, len(leafSet))
	for r := range leafSet {
		allRevs = append(allRevs, r)
	}
	winner := model.WinnerRev(allRevs)

	// Determine the full Document for the winner.
	var winnerDoc *model.Document
	if winner == doc.Rev {
		winnerDoc = doc
	} else {
		winnerDoc, _ = tx.getLeaf(doc.ID, winner)
	}

	// Update docs bucket and indices only when the winner changes.
	currentWinnerRev := ""
	if oldDoc != nil {
		currentWinnerRev = oldDoc.Rev
	}
	if winnerDoc != nil && winner != currentWinnerRev {
		if oldDoc != nil {
			for _, idx := range tx.Database.Indices() {
				if err := idx.DocumentDeleted(ctx, tx, oldDoc); err != nil {
					return err
				}
			}
		}
		if err := tx.PutRaw(ctx, []byte(doc.ID), winnerDoc); err != nil {
			return err
		}
		if winnerDoc.IsDesignDoc() {
			if err := tx.Database.BuildDesignDocIndices(ctx, tx, winnerDoc, true); err != nil {
				return err
			}
		}
		for _, idx := range tx.Database.Indices() {
			if err := idx.DocumentStored(ctx, tx, winnerDoc); err != nil {
				return err
			}
		}
	}

	return nil
}

func (tx *Transaction) DeleteDocument(ctx context.Context, docID, rev string) (*model.Document, error) {
	// Get current winner from the docs bucket.
	oldDoc, err := tx.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if oldDoc == nil {
		return nil, ErrNotFound
	}

	// If rev matches the winning revision, delegate to PutDocument (existing behavior).
	if oldDoc.Rev == rev {
		doc := &model.Document{
			ID:      docID,
			Rev:     rev,
			Deleted: true,
		}
		_, err := tx.PutDocument(ctx, doc)
		if err != nil {
			return doc, err
		}
		return doc, nil
	}

	// Rev doesn't match the winner — check if it's a conflict leaf.
	leafRevs := tx.leafRevs(docID)
	isLeaf := false
	for _, lr := range leafRevs {
		if lr == rev {
			isLeaf = true
			break
		}
	}
	if !isLeaf {
		return nil, ErrConflict
	}

	// Get the leaf doc to preserve its RevHistory.
	leafDoc, err := tx.getLeaf(docID, rev)
	if err != nil {
		return nil, ErrConflict
	}

	// Build a tombstone for this conflict branch.
	tombstone := &model.Document{
		ID:      docID,
		Rev:     rev,
		Deleted: true,
	}

	// Generate a new tombstone revision (same hash logic as PutDocument).
	revSeq := tombstone.NextSequenceRevision()
	hash := md5.New()
	err = cbor.NewEncoder(hash).Encode(tombstone)
	if err != nil {
		return nil, err
	}
	newRev := strconv.Itoa(revSeq) + "-" + hex.EncodeToString(hash.Sum(nil))
	tombstone.Rev = newRev

	// Build revision history from the leaf's history.
	oldHistory := leafDoc.RevHistory
	if len(oldHistory) == 0 && rev != "" {
		oldHistory = []string{rev}
	}
	tombstone.RevHistory = append([]string{newRev}, oldHistory...)
	if len(tombstone.RevHistory) > 1000 {
		tombstone.RevHistory = tombstone.RevHistory[:1000]
	}

	// Update leaves: remove old rev, add tombstone.
	tx.deleteLeaf(docID, rev)
	_ = tx.putLeaf(tombstone)

	// Recompute the winner across all remaining leaves.
	// Re-read committed leaves and apply our pending mutations in memory.
	committedRevs := tx.leafRevs(docID)
	leafSet := make(map[string]bool, len(committedRevs)+2)
	for _, r := range committedRevs {
		leafSet[r] = true
	}
	// bbolt deferred writes: manually apply our pending changes.
	delete(leafSet, rev)
	leafSet[newRev] = true

	allRevs := make([]string, 0, len(leafSet))
	for r := range leafSet {
		allRevs = append(allRevs, r)
	}
	winner := model.WinnerRev(allRevs)

	// Determine the full Document for the winner.
	var winnerDoc *model.Document
	switch winner {
	case newRev:
		winnerDoc = tombstone
	case oldDoc.Rev:
		winnerDoc = oldDoc
	default:
		winnerDoc, _ = tx.getLeaf(docID, winner)
	}

	// Update docs bucket and indices if the winner changed.
	if winnerDoc != nil && winner != oldDoc.Rev {
		for _, idx := range tx.Database.Indices() {
			if err := idx.DocumentDeleted(ctx, tx, oldDoc); err != nil {
				return nil, err
			}
		}
		if err := tx.PutRaw(ctx, []byte(docID), winnerDoc); err != nil {
			return nil, err
		}
		if winnerDoc.IsDesignDoc() {
			if err := tx.Database.BuildDesignDocIndices(ctx, tx, winnerDoc, true); err != nil {
				return nil, err
			}
		}
		for _, idx := range tx.Database.Indices() {
			if err := idx.DocumentStored(ctx, tx, winnerDoc); err != nil {
				return nil, err
			}
		}
	}

	return tombstone, nil
}

// GetLeaves returns all current leaf revisions of a document
// (the winner plus any conflicting branches).
func (tx *Transaction) GetLeaves(ctx context.Context, docID string) ([]*model.Document, error) {
	revs := tx.leafRevs(docID)
	docs := make([]*model.Document, 0, len(revs))
	for _, rev := range revs {
		d, err := tx.getLeaf(docID, rev)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// GetLeaf returns one specific leaf revision of a document.
// Returns (nil, nil) when the revision is not a known leaf.
func (tx *Transaction) GetLeaf(ctx context.Context, docID, rev string) (*model.Document, error) {
	d, err := tx.getLeaf(docID, rev)
	if err != nil {
		// Not found in leaves is not an error — return nil, nil.
		return nil, nil
	}
	return d, nil
}
