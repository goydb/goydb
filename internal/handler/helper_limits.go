package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/goydb/goydb/pkg/port"
)

// ErrLimitExceeded is returned by limitedReadCloser when the read limit is exceeded.
var ErrLimitExceeded = errors.New("entity too large")

// configInt64 reads a config value from the given section/key and parses it as int64.
// Returns 0 if absent, empty, or unparseable (0 means unlimited).
func configInt64(config *ConfigStore, section, key string) int64 {
	v, ok := config.Get(section, key)
	if !ok || v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// CheckMaxDatabases checks if the maximum number of databases has been reached.
// Returns true (and writes a 412 error) if the limit is exceeded.
func CheckMaxDatabases(w http.ResponseWriter, config *ConfigStore, ctx context.Context, storage port.Storage) bool {
	limit := configInt64(config, "couchdb", "max_dbs")
	if limit <= 0 {
		return false
	}
	dbs, err := storage.Databases(ctx)
	if err != nil {
		return false
	}
	if int64(len(dbs)) >= limit {
		WriteError(w, http.StatusPreconditionFailed, "You've exceeded the maximum number of allowed databases.")
		return true
	}
	return false
}

// CheckMaxDocumentSize checks if a document body exceeds the max_document_size limit.
// Returns true (and writes a 413 error) if the limit is exceeded.
func CheckMaxDocumentSize(w http.ResponseWriter, config *ConfigStore, bodySize int64) bool {
	limit := configInt64(config, "couchdb", "max_document_size")
	if limit <= 0 {
		return false
	}
	if bodySize > limit {
		WriteError(w, http.StatusRequestEntityTooLarge, "document_too_large")
		return true
	}
	return false
}

// CheckMaxDocsPerDB checks if adding additionalDocs would exceed the per-database document limit.
// Returns true (and writes a 412 error) if the limit would be exceeded.
func CheckMaxDocsPerDB(w http.ResponseWriter, config *ConfigStore, ctx context.Context, db port.Database, additionalDocs int64) bool {
	limit := configInt64(config, "couchdb", "max_docs_per_db")
	if limit <= 0 {
		return false
	}
	stats, err := db.Stats(ctx)
	if err != nil {
		return false
	}
	if int64(stats.DocCount)+additionalDocs > limit {
		WriteError(w, http.StatusPreconditionFailed, "You've exceeded the maximum number of documents allowed per database.")
		return true
	}
	return false
}

// CheckMaxAttachmentSize checks if an attachment exceeds the max_attachment_size limit.
// Returns true (and writes a 413 error) if the limit is exceeded.
func CheckMaxAttachmentSize(w http.ResponseWriter, config *ConfigStore, size int64) bool {
	limit := configInt64(config, "couchdb", "max_attachment_size")
	if limit <= 0 {
		return false
	}
	if size > limit {
		WriteError(w, http.StatusRequestEntityTooLarge, "attachment_too_large")
		return true
	}
	return false
}

// CheckMaxDBSize checks if the database file size has reached the max_db_size limit.
// Returns true (and writes a 412 error) if the limit is exceeded.
func CheckMaxDBSize(w http.ResponseWriter, config *ConfigStore, ctx context.Context, db port.Database) bool {
	limit := configInt64(config, "couchdb", "max_db_size")
	if limit <= 0 {
		return false
	}
	stats, err := db.Stats(ctx)
	if err != nil {
		return false
	}
	if int64(stats.FileSize) >= limit {
		WriteError(w, http.StatusPreconditionFailed, "You've exceeded the maximum allowed database size.")
		return true
	}
	return false
}

// limitedReadCloser wraps an io.ReadCloser and enforces a byte limit.
// Once the limit is exceeded, Read returns ErrLimitExceeded.
type limitedReadCloser struct {
	rc      io.ReadCloser
	limit   int64
	counted int64
}

// newLimitedReadCloser wraps rc with a byte limit. If limit <= 0, returns rc unchanged.
func newLimitedReadCloser(rc io.ReadCloser, limit int64) io.ReadCloser {
	if limit <= 0 {
		return rc
	}
	return &limitedReadCloser{rc: rc, limit: limit}
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.rc.Read(p)
	l.counted += int64(n)
	if l.counted > l.limit {
		return n, ErrLimitExceeded
	}
	return n, err
}

func (l *limitedReadCloser) Close() error {
	return l.rc.Close()
}
