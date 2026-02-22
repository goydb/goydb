package index

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

var _ port.DocumentIndex = (*MangoIndex)(nil)
var _ port.DocumentIndexSourceUpdate = (*MangoIndex)(nil)

// MangoIndex is a bbolt-backed index for Mango (_find) queries.
// It stores one entry per document keyed by length-prefixed CBOR field values
// followed by the document ID, enabling O(log n) equality lookups.
//
// Main bucket key format (allows prefix scans per equality predicate):
//
//	uint16_BE(len(cbor_f1)) | cbor_f1 | ... | uint16_BE(len(cbor_fN)) | cbor_fN | docID
//
// Invalidation bucket: docID → main bucket key (for O(1) delete/update).
type MangoIndex struct {
	ddfn          *model.DesignDocFn
	fields        []string
	bucketName    []byte
	invBucketName []byte
	mu            sync.RWMutex
	logger        port.Logger
}

func NewMangoIndex(ddfn *model.DesignDocFn, logger port.Logger) *MangoIndex {
	return &MangoIndex{
		ddfn:          ddfn,
		bucketName:    ddfn.Bucket(),
		invBucketName: append(ddfn.Bucket(), []byte(":inv")...),
		logger:        logger,
	}
}

// Fields returns the indexed field names.
func (i *MangoIndex) Fields() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.fields
}

// SourceType implements port.DocumentIndexSourceUpdate.
func (i *MangoIndex) SourceType() model.FnType {
	return model.MangoFn
}

// UpdateSource implements port.DocumentIndexSourceUpdate.
// It updates the field list from the Function definition (no view server needed).
func (i *MangoIndex) UpdateSource(_ context.Context, _ *model.Document, vf *model.Function) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.fields = vf.MangoFields
	return nil
}

// Ensure implements port.DocumentIndex.
func (i *MangoIndex) Ensure(_ context.Context, tx port.EngineWriteTransaction) error {
	tx.EnsureBucket(i.bucketName)
	tx.EnsureBucket(i.invBucketName)
	return nil
}

// Remove implements port.DocumentIndex.
func (i *MangoIndex) Remove(_ context.Context, tx port.EngineWriteTransaction) error {
	tx.DeleteBucket(i.bucketName)
	tx.DeleteBucket(i.invBucketName)
	return nil
}

// Stats implements port.DocumentIndex.
func (i *MangoIndex) Stats(_ context.Context, tx port.EngineReadTransaction) (*model.IndexStats, error) {
	s := tx.BucketStats(i.bucketName)
	si := tx.BucketStats(i.invBucketName)
	s.Allocated += si.Allocated
	s.Used += si.Used
	return s, nil
}

// DocumentStored implements port.DocumentIndex.
func (i *MangoIndex) DocumentStored(ctx context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	return i.UpdateStored(ctx, tx, []*model.Document{doc})
}

// UpdateStored implements port.DocumentIndex.
func (i *MangoIndex) UpdateStored(ctx context.Context, tx port.EngineWriteTransaction, docs []*model.Document) error {
	i.mu.RLock()
	fields := i.fields
	i.mu.RUnlock()

	for _, doc := range docs {
		if doc == nil {
			continue
		}

		// 1. Remove previous index entry via invalidation bucket.
		if oldKey, err := tx.Get(i.invBucketName, []byte(doc.ID)); err == nil && len(oldKey) > 0 {
			tx.Delete(i.bucketName, oldKey)
		}
		tx.Delete(i.invBucketName, []byte(doc.ID))

		// 2. Skip deleted, design, and local documents.
		if doc.Deleted || doc.IsDesignDoc() || doc.IsLocalDoc() {
			continue
		}

		// 3. Build main bucket key.
		newKey, err := buildMangoKey(fields, doc)
		if err != nil {
			i.logger.Warnf(ctx, "mango index: failed to build key", "doc", doc.ID, "error", err)
			continue
		}

		tx.Put(i.bucketName, newKey, nil)
		tx.Put(i.invBucketName, []byte(doc.ID), newKey)
	}

	return nil
}

// DocumentDeleted implements port.DocumentIndex.
func (i *MangoIndex) DocumentDeleted(_ context.Context, tx port.EngineWriteTransaction, doc *model.Document) error {
	if doc == nil {
		return nil
	}
	if oldKey, err := tx.Get(i.invBucketName, []byte(doc.ID)); err == nil && len(oldKey) > 0 {
		tx.Delete(i.bucketName, oldKey)
	}
	tx.Delete(i.invBucketName, []byte(doc.ID))
	return nil
}

// IteratorOptions implements port.DocumentIndex.
// MangoIndex is not designed for standard iterator access; use LookupEq instead.
func (i *MangoIndex) IteratorOptions(_ context.Context) (*model.IteratorOptions, error) {
	return nil, fmt.Errorf("MangoIndex does not support IteratorOptions; use LookupEq")
}

// LookupEq returns the IDs of all documents whose index fields match the given
// values (equality prefix scan). len(values) must equal the number of indexed
// fields, or you can pass a prefix of the fields for a partial match.
func (i *MangoIndex) LookupEq(ctx context.Context, tx port.EngineReadTransaction, values []interface{}) ([]string, error) {
	i.mu.RLock()
	fields := i.fields
	i.mu.RUnlock()

	// Build the key prefix from the supplied values.
	prefix, err := buildMangoPrefix(fields[:len(values)], values)
	if err != nil {
		return nil, err
	}

	c := tx.Cursor(i.bucketName)
	var ids []string
	for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
		// docID follows the field-value portion.
		docID := string(k[len(prefix):])
		ids = append(ids, docID)
	}
	return ids, nil
}

// buildMangoKey encodes all index fields from doc into the main bucket key format.
func buildMangoKey(fields []string, doc *model.Document) ([]byte, error) {
	values := make([]interface{}, len(fields))
	for idx, f := range fields {
		values[idx] = doc.Field(f)
	}
	prefix, err := buildMangoPrefix(fields, values)
	if err != nil {
		return nil, err
	}
	// Append docID after the field portion.
	key := append(prefix, []byte(doc.ID)...)
	return key, nil
}

// buildMangoPrefix encodes the supplied field values into the length-prefixed
// CBOR prefix used for both storing and seeking in the main bucket.
func buildMangoPrefix(fields []string, values []interface{}) ([]byte, error) {
	var buf []byte
	for i, v := range values {
		// Normalise nil to a stable CBOR null.
		_ = fields[i] // bounds check
		encoded, err := cbor.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cbor.Marshal field %q: %w", fields[i], err)
		}
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(encoded)))
		buf = append(buf, lenBuf[:]...)
		buf = append(buf, encoded...)
	}
	return buf, nil
}
