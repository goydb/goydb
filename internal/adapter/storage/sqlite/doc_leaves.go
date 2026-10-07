//go:build sqlite

package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// docLeavesTable implements bucketTable for the "doc_leaves" bucket, the one
// case where decomposing the key into real columns is required (not just
// possible) for a meaningful foreign key: the original key is always
// docID + 0x00 + rev (internal/adapter/storage/db_trx.go's leafKey), so a
// plain `key BLOB PRIMARY KEY REFERENCES documents(key)` could never match —
// `key` here is never equal to a documents.key value, only a prefix of one
// followed by 0x00 and a rev. Splitting into (doc_id, rev) columns lets the
// FK reference just the doc_id part.
type docLeavesTable struct{}

// splitLeafKey decomposes docID+0x00+rev. Every write to this bucket goes
// through leafKey (confirmed by exhaustive grep of internal/adapter/storage
// and internal/adapter/index), which never omits the 0x00 separator, so this
// is safe for Get/Put/Delete, which always receive a complete key. Seek
// (used for a docID-only prefix scan) is handled separately in the cursor
// below without needing to split anything.
func splitLeafKey(key []byte) (docID, rev []byte) {
	i := bytes.IndexByte(key, 0)
	if i < 0 {
		return key, nil
	}
	return key[:i], key[i+1:]
}

func joinLeafKey(docID, rev []byte) []byte {
	k := make([]byte, 0, len(docID)+1+len(rev))
	k = append(k, docID...)
	k = append(k, 0)
	k = append(k, rev...)
	return k
}

func (t docLeavesTable) get(ctx context.Context, ex execer, key []byte) ([]byte, error) {
	docID, rev := splitLeafKey(key)
	var value []byte
	err := ex.QueryRowContext(ctx, `SELECT value FROM doc_leaves WHERE doc_id = ? AND rev = ?`, docID, rev).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get from doc_leaves: %w", err)
	}
	return value, nil
}

func (t docLeavesTable) put(ctx context.Context, ex execer, key, value []byte) error {
	docID, rev := splitLeafKey(key)
	_, err := ex.ExecContext(ctx,
		`INSERT INTO doc_leaves (doc_id, rev, value) VALUES (?, ?, ?)
		 ON CONFLICT (doc_id, rev) DO UPDATE SET value = excluded.value`,
		docID, rev, value)
	if err != nil {
		return fmt.Errorf("sqlite: put into doc_leaves: %w", err)
	}
	return nil
}

func (t docLeavesTable) del(ctx context.Context, ex execer, key []byte) error {
	docID, rev := splitLeafKey(key)
	_, err := ex.ExecContext(ctx, `DELETE FROM doc_leaves WHERE doc_id = ? AND rev = ?`, docID, rev)
	if err != nil {
		return fmt.Errorf("sqlite: delete from doc_leaves: %w", err)
	}
	return nil
}

func (t docLeavesTable) cursor(ex execer) port.EngineCursor {
	return &docLeavesCursor{ex: ex}
}

func (t docLeavesTable) stats(ctx context.Context, ex execer) *model.IndexStats {
	var keys uint64
	var used sql.NullInt64
	_ = ex.QueryRowContext(ctx,
		`SELECT COUNT(*), SUM(LENGTH(doc_id) + LENGTH(rev) + LENGTH(value)) FROM doc_leaves`,
	).Scan(&keys, &used)
	return &model.IndexStats{
		Keys:      keys,
		Documents: keys,
		Used:      uint64(used.Int64),
		Allocated: uint64(used.Int64),
	}
}

// compositeKeyExpr is the SQL expression reconstructing the original
// docID+0x00+rev byte key from the decomposed columns, for comparison
// against a raw-bytes seek argument. The explicit CAST to BLOB matters: `||`
// gives its result TEXT storage class even when both operands are BLOB, and
// SQLite ranks BLOB > TEXT when comparing values of different storage
// classes — without the cast, this expression would always compare as
// "less than" any BLOB-bound parameter, regardless of byte content.
const compositeKeyExpr = `CAST(doc_id || X'00' || rev AS BLOB)`

const (
	leafFirstQuery = `SELECT doc_id, rev, value FROM doc_leaves ORDER BY doc_id ASC, rev ASC LIMIT ?`
	leafLastQuery  = `SELECT doc_id, rev, value FROM doc_leaves ORDER BY doc_id DESC, rev DESC LIMIT ?`
	leafSeekQuery  = `SELECT doc_id, rev, value FROM doc_leaves WHERE ` + compositeKeyExpr + ` >= ? ORDER BY doc_id ASC, rev ASC LIMIT 1`
	leafNextQuery  = `SELECT doc_id, rev, value FROM doc_leaves WHERE ` + compositeKeyExpr + ` > ? ORDER BY doc_id ASC, rev ASC LIMIT ?`
	leafPrevQuery  = `SELECT doc_id, rev, value FROM doc_leaves WHERE ` + compositeKeyExpr + ` < ? ORDER BY doc_id DESC, rev DESC LIMIT ?`
)

type leafRow struct{ docID, rev, value []byte }

// docLeavesCursor reproduces EngineCursor semantics over the decomposed
// (doc_id, rev) columns, batching fetches exactly like genericCursor (see
// its doc comment for why that's safe and why Seek stays single-row).
// First/Last/Next/Prev operate on the natural (doc_id, rev) tuple order,
// which is byte-identical to the original composite key's order (0x00 is
// the minimum byte, so a doc_id that's a strict prefix of another always
// sorts first either way — see schema.go's doc comment).
type docLeavesCursor struct {
	ex               execer
	curDocID, curRev []byte
	have             bool

	buf    []leafRow
	bufPos int
	dir    int8 // 0 = unknown (post-Seek), +1 = forward, -1 = backward
}

func (c *docLeavesCursor) popBuffered() (key, value []byte, ok bool) {
	if c.bufPos >= len(c.buf) {
		return nil, nil, false
	}
	row := c.buf[c.bufPos]
	c.bufPos++
	c.curDocID, c.curRev, c.have = row.docID, row.rev, true
	return joinLeafKey(row.docID, row.rev), row.value, true
}

func (c *docLeavesCursor) fetchBatch(query string, args ...any) (key, value []byte) {
	rows, err := c.ex.QueryContext(context.Background(), query, append(args, cursorBatchSize)...)
	if err != nil {
		c.have = false
		return nil, nil
	}
	defer rows.Close()

	c.buf = c.buf[:0]
	for rows.Next() {
		var docID, rev, v []byte
		if err := rows.Scan(&docID, &rev, &v); err != nil {
			c.have = false
			return nil, nil
		}
		c.buf = append(c.buf, leafRow{docID, rev, v})
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

func (c *docLeavesCursor) fetchOne(query string, args ...any) (key, value []byte) {
	c.buf, c.bufPos = c.buf[:0], 0
	var docID, rev, v []byte
	err := c.ex.QueryRowContext(context.Background(), query, args...).Scan(&docID, &rev, &v)
	if err != nil {
		c.have = false
		return nil, nil
	}
	c.curDocID, c.curRev, c.have = docID, rev, true
	return joinLeafKey(docID, rev), v
}

func (c *docLeavesCursor) First() (key, value []byte) {
	c.dir = +1
	return c.fetchBatch(leafFirstQuery)
}

func (c *docLeavesCursor) Last() (key, value []byte) {
	c.dir = -1
	return c.fetchBatch(leafLastQuery)
}

func (c *docLeavesCursor) Seek(seek []byte) (key, value []byte) {
	c.dir = 0
	return c.fetchOne(leafSeekQuery, seek)
}

func (c *docLeavesCursor) Next() (key, value []byte) {
	if !c.have {
		return nil, nil
	}
	if c.dir == +1 {
		if k, v, ok := c.popBuffered(); ok {
			return k, v
		}
	}
	c.dir = +1
	return c.fetchBatch(leafNextQuery, joinLeafKey(c.curDocID, c.curRev))
}

func (c *docLeavesCursor) Prev() (key, value []byte) {
	if !c.have {
		return nil, nil
	}
	if c.dir == -1 {
		if k, v, ok := c.popBuffered(); ok {
			return k, v
		}
	}
	c.dir = -1
	return c.fetchBatch(leafPrevQuery, joinLeafKey(c.curDocID, c.curRev))
}
