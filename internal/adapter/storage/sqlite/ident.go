//go:build sqlite

package sqlite

import "strings"

// sanitizeIdent reduces an arbitrary bucket name to a string safe for use as
// (part of) a SQL identifier, by replacing every byte outside
// [A-Za-z0-9_] with '_'. Used by buckets.go's registerDynamicTable to turn
// a bucket name like "views:myddoc:myview" into "views_myddoc_myview".
func sanitizeIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// quoteIdent double-quotes a SQL identifier, doubling any embedded double
// quote — standard SQL identifier escaping (distinct from Go's %q, which
// uses Go string-escape rules and would be wrong here).
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
