//go:build sqlite

package sqlite

import "sync"

// bucketCache remembers bucket-name -> table-name mappings for dynamic
// buckets (one per persisted view/mango index) so resolveBucket doesn't have
// to query the bucket_tables admin table on every Get/Put/Cursor/Delete/
// BucketStats call — the mapping never changes once a bucket's table exists.
//
// Entries are only ever added once the mapping is durably committed (see
// WriteTransaction.postCommit in write_trx.go) — never while a write
// transaction's replay is still in flight, since that transaction could
// still roll back (a later op in the same log, or the final deferred-FK
// COMMIT itself) and a cache entry for a table that turned out not to exist
// would be worse than the query it's meant to avoid.
type bucketCache struct {
	mu sync.RWMutex
	m  map[string]string
}

func newBucketCache() *bucketCache {
	return &bucketCache{m: make(map[string]string)}
}

func (c *bucketCache) get(name string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	table, ok := c.m[name]
	return table, ok
}

func (c *bucketCache) set(name, table string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[name] = table
}

func (c *bucketCache) delete(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, name)
}
