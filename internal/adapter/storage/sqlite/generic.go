//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// cursorBatchSize bounds how many rows a single cursor fetch pulls back at
// once (see genericCursor/docLeavesCursor). Without batching, an N-row
// iteration is N single-row SQL statements — a real bottleneck on large
// views/changes feeds. 128 is a modest, arbitrary tradeoff between fewer
// round-trips and not over-fetching for iterations that stop early.
const cursorBatchSize = 128

// genericTable implements bucketTable for the (key BLOB PRIMARY KEY, value
// BLOB) shape shared by every well-known table except doc_leaves, and by
// every dynamic (view/mango index) bucket table — see buckets.go for which
// bucket names map here. Any foreign key the table declares lives purely in
// its CREATE TABLE DDL (schema.go, or the generated DDL in buckets.go for
// dynamic tables); it doesn't change anything here.
type genericTable struct {
	name string
}

func (t genericTable) get(ctx context.Context, ex execer, key []byte) ([]byte, error) {
	var value []byte
	err := ex.QueryRowContext(ctx, fmt.Sprintf(`SELECT value FROM %s WHERE key = ?`, quoteIdent(t.name)), key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get from %q: %w", t.name, err)
	}
	return value, nil
}

func (t genericTable) put(ctx context.Context, ex execer, key, value []byte) error {
	_, err := ex.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		quoteIdent(t.name)), key, value)
	if err != nil {
		return fmt.Errorf("sqlite: put into %q: %w", t.name, err)
	}
	return nil
}

func (t genericTable) del(ctx context.Context, ex execer, key []byte) error {
	_, err := ex.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE key = ?`, quoteIdent(t.name)), key)
	if err != nil {
		return fmt.Errorf("sqlite: delete from %q: %w", t.name, err)
	}
	return nil
}

func (t genericTable) cursor(ex execer) port.EngineCursor {
	return newGenericCursor(ex, t.name)
}

func (t genericTable) stats(ctx context.Context, ex execer) *model.IndexStats {
	var keys uint64
	var used sql.NullInt64
	_ = ex.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*), SUM(LENGTH(key) + LENGTH(value)) FROM %s`, quoteIdent(t.name)),
	).Scan(&keys, &used)
	return &model.IndexStats{
		Keys:      keys,
		Documents: keys,
		Used:      uint64(used.Int64),
		Allocated: uint64(used.Int64),
	}
}

type kvPair struct{ key, value []byte }

// genericCursor walks a genericTable's rows in byte-lexicographic key
// order (SQLite's default BLOB comparison). Like bbolt's cursor, it has no
// notion of "no such bucket" — an empty or not-yet-created table (see
// emptyTable in buckets.go) simply yields no rows.
//
// First/Last/Next/Prev fetch cursorBatchSize rows at a time and serve
// subsequent same-direction steps from that buffer instead of querying
// per row — safe because internal/adapter/storage/db_iter.go's Iterator,
// the only real caller, fixes a direction at construction and never mixes
// Next()/Prev() calls on one cursor. Seek stays a single-row query: its
// direction is genuinely unknown (both an ascending Seek+Next and the
// descending "pad+Prev" trick in db_iter.go follow a Seek elsewhere in this
// codebase, so prefetching a guess would be wrong exactly as often as
// right) — but it still clears the buffer so a subsequent Next()/Prev()
// can't reuse stale rows from a prior position. A direction change
// mid-iteration (never exercised today, but not assumed away) just costs a
// fresh batched fetch instead of a free buffer pop — correct, not worse
// than the unbatched behavior this replaces.
type genericCursor struct {
	ex    execer
	table string

	cur  []byte
	have bool

	buf    []kvPair
	bufPos int
	dir    int8 // 0 = unknown (post-Seek), +1 = forward, -1 = backward

	qFirst, qLast, qSeek, qNext, qPrev string
}

func newGenericCursor(ex execer, table string) *genericCursor {
	q := quoteIdent(table)
	return &genericCursor{
		ex:     ex,
		table:  table,
		qFirst: fmt.Sprintf(`SELECT key, value FROM %s ORDER BY key ASC LIMIT ?`, q),
		qLast:  fmt.Sprintf(`SELECT key, value FROM %s ORDER BY key DESC LIMIT ?`, q),
		qSeek:  fmt.Sprintf(`SELECT key, value FROM %s WHERE key >= ? ORDER BY key ASC LIMIT 1`, q),
		qNext:  fmt.Sprintf(`SELECT key, value FROM %s WHERE key > ? ORDER BY key ASC LIMIT ?`, q),
		qPrev:  fmt.Sprintf(`SELECT key, value FROM %s WHERE key < ? ORDER BY key DESC LIMIT ?`, q),
	}
}

// popBuffered returns the next already-fetched row, if any, without a query.
func (c *genericCursor) popBuffered() (key, value []byte, ok bool) {
	if c.bufPos >= len(c.buf) {
		return nil, nil, false
	}
	row := c.buf[c.bufPos]
	c.bufPos++
	c.cur, c.have = row.key, true
	return row.key, row.value, true
}

// fetchBatch runs a multi-row query (ending in "... LIMIT ?"), replacing
// the buffer with up to cursorBatchSize rows, and returns the first one.
func (c *genericCursor) fetchBatch(query string, args ...any) (key, value []byte) {
	rows, err := c.ex.QueryContext(context.Background(), query, append(args, cursorBatchSize)...)
	if err != nil {
		c.have = false
		return nil, nil
	}
	defer rows.Close()

	c.buf = c.buf[:0]
	for rows.Next() {
		var k, v []byte
		if err := rows.Scan(&k, &v); err != nil {
			c.have = false
			return nil, nil
		}
		c.buf = append(c.buf, kvPair{k, v})
	}
	if err := rows.Err(); err != nil {
		c.have = false
		return nil, nil
	}

	c.bufPos = 0
	if len(c.buf) == 0 {
		c.have = false
		return nil, nil
	}
	key, value, _ = c.popBuffered()
	return key, value
}

// fetchOne runs a single-row query — only Seek uses this, see the cursor's
// doc comment for why Seek doesn't batch.
func (c *genericCursor) fetchOne(query string, args ...any) (key, value []byte) {
	c.buf, c.bufPos = c.buf[:0], 0
	err := c.ex.QueryRowContext(context.Background(), query, args...).Scan(&key, &value)
	if err != nil {
		c.have = false
		return nil, nil
	}
	c.cur, c.have = key, true
	return key, value
}

func (c *genericCursor) First() (key, value []byte) {
	c.dir = +1
	return c.fetchBatch(c.qFirst)
}

func (c *genericCursor) Last() (key, value []byte) {
	c.dir = -1
	return c.fetchBatch(c.qLast)
}

func (c *genericCursor) Seek(seek []byte) (key, value []byte) {
	c.dir = 0
	return c.fetchOne(c.qSeek, seek)
}

func (c *genericCursor) Next() (key, value []byte) {
	if !c.have {
		return nil, nil
	}
	if c.dir == +1 {
		if k, v, ok := c.popBuffered(); ok {
			return k, v
		}
	}
	c.dir = +1
	return c.fetchBatch(c.qNext, c.cur)
}

func (c *genericCursor) Prev() (key, value []byte) {
	if !c.have {
		return nil, nil
	}
	if c.dir == -1 {
		if k, v, ok := c.popBuffered(); ok {
			return k, v
		}
	}
	c.dir = -1
	return c.fetchBatch(c.qPrev, c.cur)
}
