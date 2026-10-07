//go:build sqlite

package sqlite

// schema creates one real table per well-known bucket instead of a single
// shared kv table, plus two small admin tables: bucket_tables (maps dynamic
// per-view/per-index bucket names to generated table names, see buckets.go)
// and seq (per-bucket sequence counters for PutWithSequence/
// PutWithReusedSequence, orthogonal to which table a bucket's data lives in).
//
// Every well-known table uses the same (key BLOB PRIMARY KEY, value BLOB)
// shape as the generic dynamic-bucket tables (see generic.go) — the only
// exception is doc_leaves, whose composite key (docID + 0x00 + rev) is
// decomposed into (doc_id, rev) columns so a foreign key can reference just
// the docID part (see doc_leaves.go). Foreign keys are declared
// DEFERRABLE INITIALLY DEFERRED so they're checked once, at the COMMIT that
// ends a write transaction's replay phase, regardless of what order the
// op log happens to apply inserts across tables within that transaction.
const schema = `
CREATE TABLE IF NOT EXISTS documents (
	key   BLOB PRIMARY KEY,
	value BLOB NOT NULL
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS doc_leaves (
	doc_id BLOB NOT NULL REFERENCES documents(key) DEFERRABLE INITIALLY DEFERRED,
	rev    BLOB NOT NULL,
	value  BLOB NOT NULL,
	PRIMARY KEY (doc_id, rev)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS changes (
	key   BLOB PRIMARY KEY,
	value BLOB NOT NULL REFERENCES documents(key) DEFERRABLE INITIALLY DEFERRED
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS changes_invalidation (
	key   BLOB PRIMARY KEY REFERENCES documents(key) DEFERRABLE INITIALLY DEFERRED,
	value BLOB NOT NULL
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS deleted_docs (
	key   BLOB PRIMARY KEY REFERENCES documents(key) DEFERRABLE INITIALLY DEFERRED,
	value BLOB
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS att_refs (
	key   BLOB PRIMARY KEY,
	value BLOB
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS meta (
	key   BLOB PRIMARY KEY,
	value BLOB
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS internal_docs (
	key   BLOB PRIMARY KEY,
	value BLOB
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS tasks (
	key   BLOB PRIMARY KEY,
	value BLOB
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS bucket_tables (
	bucket     BLOB PRIMARY KEY,
	table_name TEXT NOT NULL UNIQUE
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS seq (
	bucket BLOB PRIMARY KEY,
	value  INTEGER NOT NULL
) WITHOUT ROWID;
`
