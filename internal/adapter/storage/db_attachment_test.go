package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openAttStorage opens a temporary storage, creates "testdb", and returns the
// concrete *Database so tests can access unexported helpers (blobPath, etc.).
func openAttStorage(t *testing.T) (dir string, s *Storage, db *Database, cleanup func()) {
	t.Helper()
	var err error
	dir, err = os.MkdirTemp(os.TempDir(), "goydb-att-test-*")
	require.NoError(t, err)

	s, err = Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	cleanup = func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
	return dir, s, s.dbs["testdb"], cleanup
}

// putDocAndAtt creates a document then adds an attachment to it, returning the
// doc rev after the attachment write and the stored blob digest.
func putDocAndAtt(t *testing.T, db *Database, docID, filename, content string) (rev, digest string) {
	t.Helper()
	ctx := context.Background()

	docRev, err := db.PutDocument(ctx, &model.Document{
		ID:   docID,
		Data: map[string]interface{}{"x": 1},
	})
	require.NoError(t, err)

	att := &model.Attachment{
		Filename:    filename,
		ContentType: "text/plain",
		Reader:      io.NopCloser(strings.NewReader(content)),
		ExpectedRev: docRev,
	}
	rev, err = db.PutAttachment(ctx, docID, att)
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, docID)
	require.NoError(t, err)
	return rev, doc.Attachments[filename].Digest
}

func TestPutAttachment_BlobAtDigestPath(t *testing.T) {
	_, _, db, cleanup := openAttStorage(t)
	defer cleanup()

	_, digest := putDocAndAtt(t, db, "doc1", "file.txt", "hello")

	// Blob must exist at the content-addressed path.
	blobPath := db.blobPath(digest)
	_, err := os.Stat(blobPath)
	assert.NoError(t, err, "blob should exist at digest path %s", blobPath)

	// Old per-document path must NOT exist.
	oldPath := filepath.Join(db.databaseDir, AttachmentDir, "doc1", "file.txt")
	_, err = os.Stat(oldPath)
	assert.True(t, os.IsNotExist(err), "old per-doc path should not exist")
}

func TestPutAttachment_Deduplication(t *testing.T) {
	_, _, db, cleanup := openAttStorage(t)
	defer cleanup()

	ctx := context.Background()

	// doc1 gets the attachment.
	_, digest1 := putDocAndAtt(t, db, "doc1", "file.txt", "shared content")

	// doc2 gets the same content — same digest, same blob.
	docRev2, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"y": 2},
	})
	require.NoError(t, err)

	att2 := &model.Attachment{
		Filename:    "file.txt",
		ContentType: "text/plain",
		Reader:      io.NopCloser(strings.NewReader("shared content")),
		ExpectedRev: docRev2,
	}
	_, err = db.PutAttachment(ctx, "doc2", att2)
	require.NoError(t, err)

	doc2, err := db.GetDocument(ctx, "doc2")
	require.NoError(t, err)
	digest2 := doc2.Attachments["file.txt"].Digest

	assert.Equal(t, digest1, digest2, "identical content should share the same digest")

	// Both attachments must be readable.
	r1, err := db.GetAttachment(ctx, "doc1", "file.txt")
	require.NoError(t, err)
	data1, err := io.ReadAll(r1.Reader)
	require.NoError(t, err)
	_ = r1.Reader.Close()
	assert.Equal(t, "shared content", string(data1))

	r2, err := db.GetAttachment(ctx, "doc2", "file.txt")
	require.NoError(t, err)
	data2, err := io.ReadAll(r2.Reader)
	require.NoError(t, err)
	_ = r2.Reader.Close()
	assert.Equal(t, "shared content", string(data2))
}

func TestDeleteAttachment_DecrementKeepsBlob(t *testing.T) {
	_, _, db, cleanup := openAttStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Both docs share the same blob (ref count = 2).
	_, digest := putDocAndAtt(t, db, "doc1", "file.txt", "shared content")

	docRev2, err := db.PutDocument(ctx, &model.Document{
		ID:   "doc2",
		Data: map[string]interface{}{"y": 2},
	})
	require.NoError(t, err)
	att := &model.Attachment{
		Filename:    "file.txt",
		ContentType: "text/plain",
		Reader:      io.NopCloser(strings.NewReader("shared content")),
		ExpectedRev: docRev2,
	}
	_, err = db.PutAttachment(ctx, "doc2", att)
	require.NoError(t, err)

	// Delete doc1's attachment — ref count drops to 1.
	doc1, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	_, err = db.DeleteAttachment(ctx, "doc1", "file.txt", doc1.Rev)
	require.NoError(t, err)

	// Blob file must still be present (doc2 still holds a reference).
	blobPath := db.blobPath(digest)
	_, err = os.Stat(blobPath)
	assert.NoError(t, err, "blob should persist while doc2 still references it")
}

func TestDeleteAttachment_LastRefRemovesBlob(t *testing.T) {
	_, _, db, cleanup := openAttStorage(t)
	defer cleanup()

	ctx := context.Background()

	_, digest := putDocAndAtt(t, db, "doc1", "file.txt", "only here")

	blobPath := db.blobPath(digest)
	_, err := os.Stat(blobPath)
	require.NoError(t, err, "blob should exist before delete")

	doc1, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	_, err = db.DeleteAttachment(ctx, "doc1", "file.txt", doc1.Rev)
	require.NoError(t, err)

	_, err = os.Stat(blobPath)
	assert.True(t, os.IsNotExist(err), "blob should be removed when the last reference is deleted")
}

func TestPutAttachment_ReplaceContent(t *testing.T) {
	_, _, db, cleanup := openAttStorage(t)
	defer cleanup()

	ctx := context.Background()

	rev1, digestOld := putDocAndAtt(t, db, "doc1", "file.txt", "old content")

	blobOld := db.blobPath(digestOld)
	_, err := os.Stat(blobOld)
	require.NoError(t, err, "old blob should exist before replacement")

	// Overwrite with different content.
	att := &model.Attachment{
		Filename:    "file.txt",
		ContentType: "text/plain",
		Reader:      io.NopCloser(strings.NewReader("new content")),
		ExpectedRev: rev1,
	}
	_, err = db.PutAttachment(ctx, "doc1", att)
	require.NoError(t, err)

	doc, err := db.GetDocument(ctx, "doc1")
	require.NoError(t, err)
	digestNew := doc.Attachments["file.txt"].Digest

	// Old blob must be gone (ref count hit 0).
	_, err = os.Stat(blobOld)
	assert.True(t, os.IsNotExist(err), "old blob should be removed after replacement")

	// New blob must be present.
	blobNew := db.blobPath(digestNew)
	_, err = os.Stat(blobNew)
	assert.NoError(t, err, "new blob should exist after replacement")

	// Attachment must be readable with new content.
	attR, err := db.GetAttachment(ctx, "doc1", "file.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(attR.Reader)
	require.NoError(t, err)
	_ = attR.Reader.Close()
	assert.Equal(t, "new content", string(data))
}

func TestMigrateAttachments_SentinelOnFreshDB(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "goydb-att-reopen-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ctx := context.Background()

	s, err := Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err)

	_, err = s.CreateDatabase(ctx, "testdb")
	require.NoError(t, err)

	db := s.dbs["testdb"]
	_, digest := putDocAndAtt(t, db, "doc1", "file.txt", "persist me")

	blobPath := db.blobPath(digest)
	_, statErr := os.Stat(blobPath)
	require.NoError(t, statErr, "blob must exist before reopen")

	// Close the first storage instance.
	require.NoError(t, s.Close())

	// Reopen from the same directory. migrateAttachments should find the
	// sentinel and return early without disturbing any existing blobs.
	s2, err := Open(dir, WithLogger(logger.NewNoLog()))
	require.NoError(t, err)
	defer s2.Close()

	db2 := s2.dbs["testdb"]
	require.NotNil(t, db2, "testdb should be reloaded")

	attR, err := db2.GetAttachment(ctx, "doc1", "file.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(attR.Reader)
	require.NoError(t, err)
	_ = attR.Reader.Close()
	assert.Equal(t, "persist me", string(data))
}
