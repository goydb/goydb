package client

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/goydb/goydb/pkg/model"
)

// Database is a handle to a single database on a Client's server.
type Database struct {
	client *Client
	name   string
}

// Name returns the database's name.
func (d *Database) Name() string { return d.name }

func (d *Database) path(suffix string) string {
	return "/" + url.PathEscape(d.name) + suffix
}

// Raw is Client.Raw with path relative to this database (e.g. "/mydoc"
// rather than "/mydb/mydoc") -- for tests/callers needing exact wire-level
// detail (status code, headers, a deliberately malformed body) this
// database's typed methods don't expose.
func (d *Database) Raw(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	return d.client.Raw(ctx, method, d.path(path), body, headers)
}

// Create creates the database. args, if given, carries optional creation
// query parameters (e.g. "engine", "q", "n", "partitioned"); only args[0] is
// used.
func (d *Database) Create(ctx context.Context, args ...map[string]string) error {
	path := d.path("")
	if len(args) > 0 && len(args[0]) > 0 {
		q := url.Values{}
		for k, v := range args[0] {
			q.Set(k, v)
		}
		path += "?" + q.Encode()
	}
	return d.client.do(ctx, http.MethodPut, path, nil, nil)
}

// Destroy deletes the database itself (not to be confused with Delete,
// which deletes a single document).
func (d *Database) Destroy(ctx context.Context) error {
	return d.client.do(ctx, http.MethodDelete, d.path(""), nil, nil)
}

// Exists reports whether the database exists.
func (d *Database) Exists(ctx context.Context) (bool, error) {
	resp, err := d.client.request(ctx, http.MethodHead, d.path(""), nil, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err := checkStatus(resp); err != nil {
		return false, err
	}
	return true, nil
}

// DBInfo is the response of GET /{db}.
type DBInfo struct {
	DBName            string `json:"db_name"`
	DocCount          int    `json:"doc_count"`
	DocDelCount       int    `json:"doc_del_count"`
	UpdateSeq         string `json:"update_seq"`
	DiskSize          int64  `json:"disk_size"`
	DataSize          int64  `json:"data_size"`
	InstanceStartTime string `json:"instance_start_time"`
}

// Info returns the database's info document.
func (d *Database) Info(ctx context.Context) (*DBInfo, error) {
	var info DBInfo
	if err := d.client.do(ctx, http.MethodGet, d.path(""), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Compact triggers database compaction, or view-index compaction for ddoc
// when ddoc is non-empty.
func (d *Database) Compact(ctx context.Context, ddoc string) error {
	path := d.path("/_compact")
	if ddoc != "" {
		path += "/" + url.PathEscape(ddoc)
	}
	return d.client.do(ctx, http.MethodPost, path, nil, nil)
}

// ViewCleanup removes unused view index files.
func (d *Database) ViewCleanup(ctx context.Context) error {
	return d.client.do(ctx, http.MethodPost, d.path("/_view_cleanup"), nil, nil)
}

// EnsureFullCommit flushes any buffered writes to stable storage.
func (d *Database) EnsureFullCommit(ctx context.Context) error {
	return d.client.do(ctx, http.MethodPost, d.path("/_ensure_full_commit"), map[string]interface{}{}, nil)
}

// GetSecurity returns the database's security document.
func (d *Database) GetSecurity(ctx context.Context) (*model.Security, error) {
	var sec model.Security
	if err := d.client.do(ctx, http.MethodGet, d.path("/_security"), nil, &sec); err != nil {
		return nil, err
	}
	return &sec, nil
}

// PutSecurity replaces the database's security document.
func (d *Database) PutSecurity(ctx context.Context, sec *model.Security) error {
	return d.client.do(ctx, http.MethodPut, d.path("/_security"), sec, nil)
}

// GetRevsLimit returns the maximum number of document revisions kept.
func (d *Database) GetRevsLimit(ctx context.Context) (int, error) {
	var limit int
	err := d.client.do(ctx, http.MethodGet, d.path("/_revs_limit"), nil, &limit)
	return limit, err
}

// SetRevsLimit sets the maximum number of document revisions kept.
func (d *Database) SetRevsLimit(ctx context.Context, limit int) error {
	return d.client.do(ctx, http.MethodPut, d.path("/_revs_limit"), limit, nil)
}

// GetPurgedInfosLimit returns the purged-document-infos cache size limit.
func (d *Database) GetPurgedInfosLimit(ctx context.Context) (int, error) {
	var limit int
	err := d.client.do(ctx, http.MethodGet, d.path("/_purged_infos_limit"), nil, &limit)
	return limit, err
}

// SetPurgedInfosLimit sets the purged-document-infos cache size limit.
func (d *Database) SetPurgedInfosLimit(ctx context.Context, limit int) error {
	return d.client.do(ctx, http.MethodPut, d.path("/_purged_infos_limit"), limit, nil)
}

// Purge permanently removes the given document revisions (docID -> revs),
// bypassing the usual tombstone mechanism.
func (d *Database) Purge(ctx context.Context, revs map[string][]string) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := d.client.do(ctx, http.MethodPost, d.path("/_purge"), revs, &out)
	return out, err
}

// MissingRevs reports, for each document ID in revs, which of the given
// revisions the server does not have.
func (d *Database) MissingRevs(ctx context.Context, revs map[string][]string) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := d.client.do(ctx, http.MethodPost, d.path("/_missing_revs"), revs, &out)
	return out, err
}

// SyncShards triggers shard sync (single-node no-op, accepted for
// CouchDB compatibility).
func (d *Database) SyncShards(ctx context.Context) error {
	return d.client.do(ctx, http.MethodPost, d.path("/_sync_shards"), nil, nil)
}

// Shards returns shard placement info, optionally for a specific document.
func (d *Database) Shards(ctx context.Context, docID string) (map[string]interface{}, error) {
	path := d.path("/_shards")
	if docID != "" {
		path += "/" + url.PathEscape(docID)
	}
	var out map[string]interface{}
	err := d.client.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// Partition returns a handle scoped to a single partition key.
func (d *Database) Partition(key string) *Partition {
	return &Partition{db: d, key: key}
}
