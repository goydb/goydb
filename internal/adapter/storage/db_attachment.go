package storage

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

const AttachmentDir = "attachments"

// digestHex strips an optional "md5-" prefix and returns the raw hex string.
func digestHex(digest string) string {
	return strings.TrimPrefix(digest, "md5-")
}

// blobDir returns the two-level directory for a content-addressed blob.
func (d *Database) blobDir(digest string) string {
	h := digestHex(digest)
	return filepath.Join(d.databaseDir, AttachmentDir, h[0:2], h[2:4])
}

// blobPath returns the full filesystem path for a content-addressed blob.
func (d *Database) blobPath(digest string) string {
	h := digestHex(digest)
	return filepath.Join(d.databaseDir, AttachmentDir, h[0:2], h[2:4], h[4:])
}

// AttachmentReader opens the blob file for the given MD5 hex digest.
func (d *Database) AttachmentReader(digest string) (io.ReadCloser, error) {
	return os.Open(d.blobPath(digest))
}

// ---------------------------------------------------------------------------
// Ref-count helpers (operate on a bbolt write transaction via the engine tx).
// ---------------------------------------------------------------------------

func encodeRef(n int64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(n))
	return b[:]
}

func decodeRef(b []byte) int64 {
	if len(b) < 8 {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b))
}

func readAttRef(tx port.EngineReadTransaction, digest string) (int64, error) {
	data, err := tx.Get(model.AttRefsBucket, []byte(digestHex(digest)))
	if err == port.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return decodeRef(data), nil
}

// incAttRef increments the reference count for digest by 1.
func incAttRef(tx port.EngineWriteTransaction, digest string) error {
	count, err := readAttRef(tx, digest)
	if err != nil {
		return err
	}
	tx.Put(model.AttRefsBucket, []byte(digestHex(digest)), encodeRef(count+1))
	return nil
}

// decAttRef decrements the reference count for digest and returns the new
// count.  When the count reaches 0 the bucket entry is deleted (caller is
// responsible for removing the blob file after the transaction commits).
func decAttRef(tx port.EngineWriteTransaction, digest string) (int64, error) {
	count, err := readAttRef(tx, digest)
	if err != nil {
		return 0, err
	}
	newCount := count - 1
	if newCount <= 0 {
		newCount = 0
		tx.Delete(model.AttRefsBucket, []byte(digestHex(digest)))
	} else {
		tx.Put(model.AttRefsBucket, []byte(digestHex(digest)), encodeRef(newCount))
	}
	return newCount, nil
}

// ---------------------------------------------------------------------------
// Database attachment operations.
// ---------------------------------------------------------------------------

func (d *Database) PutAttachment(ctx context.Context, docID string, att *model.Attachment) (string, error) {
	defer func() { _ = att.Reader.Close() }()

	// 1. Stream content to a temp file, compute MD5 digest.
	tmpDir := filepath.Join(d.databaseDir, AttachmentDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", err
	}
	tmpFile, err := os.CreateTemp(tmpDir, "att-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once renamed

	sum := md5.New()
	n, err := io.Copy(tmpFile, io.TeeReader(att.Reader, sum))
	_ = tmpFile.Close()
	if err != nil {
		return "", err
	}
	att.Digest = hex.EncodeToString(sum.Sum(nil))
	att.Length = n
	att.Stub = true

	// 2. Move temp file into the content-addressed location (idempotent).
	if err := os.MkdirAll(d.blobDir(att.Digest), 0755); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, d.blobPath(att.Digest)); err != nil {
		return "", err
	}

	// 3. Update ref counts and document in a single bbolt transaction.
	var oldDigestToClean string
	var rev string
	err = d.Transaction(ctx, func(tx port.DatabaseTx) error {
		doc, err := tx.GetDocument(ctx, docID)
		if err != nil {
			return err
		}
		if doc == nil {
			return ErrNotFound
		}

		// Increment ref count for the new blob.
		if err := incAttRef(tx, att.Digest); err != nil {
			return err
		}

		// Decrement ref count for the blob being replaced, if any.
		if existing, ok := doc.Attachments[att.Filename]; ok && existing.Digest != att.Digest {
			count, err := decAttRef(tx, existing.Digest)
			if err != nil {
				return err
			}
			if count == 0 {
				oldDigestToClean = existing.Digest
			}
		}

		if doc.Attachments == nil {
			doc.Attachments = make(map[string]*model.Attachment)
		}
		doc.Attachments[att.Filename] = att

		if att.ExpectedRev != "" {
			doc.Rev = att.ExpectedRev
		}

		rev, err = tx.PutDocument(ctx, doc)
		return err
	})
	if err != nil {
		return "", err
	}

	// 4. Post-commit: remove the old blob if its ref count hit 0.
	if oldDigestToClean != "" {
		_ = os.Remove(d.blobPath(oldDigestToClean))
	}

	return rev, nil
}

func (d *Database) DeleteAttachment(ctx context.Context, docID, name, clientRev string) (string, error) {
	var rev string
	var blobToRemove string
	err := d.Transaction(ctx, func(tx port.DatabaseTx) error {
		doc, err := tx.GetDocument(ctx, docID)
		if err != nil {
			return err
		}
		if doc == nil {
			return ErrNotFound
		}

		att, ok := doc.Attachments[name]
		if !ok {
			return ErrNotFound
		}

		// Decrement ref count; record digest for post-commit cleanup if it hits 0.
		count, err := decAttRef(tx, att.Digest)
		if err != nil {
			return err
		}
		if count == 0 {
			blobToRemove = att.Digest
		}

		delete(doc.Attachments, name)

		if clientRev != "" {
			doc.Rev = clientRev
		}

		rev, err = tx.PutDocument(ctx, doc)
		return err
	})
	if err != nil {
		return "", err
	}

	// Post-commit: remove the blob file if no references remain.
	if blobToRemove != "" {
		_ = os.Remove(d.blobPath(blobToRemove))
	}

	return rev, nil
}

func (d *Database) GetAttachment(ctx context.Context, docID, name string) (*model.Attachment, error) {
	var attachment *model.Attachment
	err := d.Transaction(ctx, func(tx port.DatabaseTx) error {
		doc, err := tx.GetDocument(ctx, docID)
		if err != nil {
			return err
		}
		if doc == nil {
			return ErrNotFound
		}

		var ok bool
		attachment, ok = doc.Attachments[name]
		if !ok {
			return ErrNotFound
		}
		attachment.Reader, err = d.AttachmentReader(attachment.Digest)
		return err
	})
	if err != nil {
		return nil, err
	}

	return attachment, nil
}

// ---------------------------------------------------------------------------
// Migration: per-document path → content-addressed path.
// ---------------------------------------------------------------------------

// migrateAttachments moves existing attachment blobs from the old layout
// ({dbdir}/attachments/{docID}/{filename}) to the content-addressed layout
// ({dbdir}/attachments/{digest[0:2]}/{digest[2:4]}/{digest[4:]}) and builds
// the initial att_refs reference counts.  A sentinel key "_scheme"="digest"
// prevents the migration from running more than once.
func (d *Database) migrateAttachments(ctx context.Context) error {
	// Fast path: check sentinel.
	var needsMigration bool
	_ = d.db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		v, err := tx.Get(model.AttRefsBucket, []byte("_scheme"))
		if err != nil || string(v) != "digest" {
			needsMigration = true
		}
		return nil
	})
	if !needsMigration {
		return nil
	}

	// Collect all attachment metadata from stored documents.
	type attEntry struct {
		docID   string
		attName string
		digest  string
	}
	var entries []attEntry
	err := d.db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		cursor := tx.Cursor(model.DocsBucket)
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var doc model.Document
			if err := bson.Unmarshal(v, &doc); err != nil {
				continue
			}
			for name, att := range doc.Attachments {
				if att == nil || att.Digest == "" {
					continue
				}
				entries = append(entries, attEntry{
					docID:   string(k),
					attName: name,
					digest:  att.Digest,
				})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Move files from the old path to the digest path.
	for _, entry := range entries {
		oldPath := filepath.Join(d.databaseDir, AttachmentDir, entry.docID, entry.attName)
		newPath := d.blobPath(entry.digest)

		if _, statErr := os.Stat(oldPath); os.IsNotExist(statErr) {
			// Already migrated or never existed on disk.
			continue
		}
		if err := os.MkdirAll(d.blobDir(entry.digest), 0755); err != nil {
			return err
		}
		// os.Rename atomically replaces the destination; safe even when
		// multiple entries share the same digest (same content).
		_ = os.Rename(oldPath, newPath)
	}

	// Count references per digest, then write att_refs and the sentinel in a
	// single transaction.
	digestCounts := make(map[string]int64)
	for _, entry := range entries {
		digestCounts[digestHex(entry.digest)]++
	}

	return d.rawTx(func(tx *Transaction) error {
		for h, count := range digestCounts {
			tx.Put(model.AttRefsBucket, []byte(h), encodeRef(count))
		}
		tx.Put(model.AttRefsBucket, []byte("_scheme"), []byte("digest"))
		return nil
	})
}
