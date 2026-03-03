package handler

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goydb/goydb/pkg/port"
)

func intOption(name string, fallback int64, options url.Values) int64 {
	if len(options[name]) == 0 {
		return fallback
	}
	v, err := strconv.ParseInt(options[name][0], 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func boolOption(name string, fallback bool, options url.Values) bool {
	if len(options[name]) == 0 {
		return fallback
	}
	if options[name][0] == "" {
		return fallback
	}
	return options[name][0] == "true"
}

// unquoteJSON strips surrounding JSON double-quotes from a string value if
// present.  When mergeBodyIntoOptions stores POST body fields, JSON strings
// like `"ok"` keep their quotes.  This helper decodes them so downstream
// comparisons work correctly (e.g. stale == "ok" instead of stale == `"ok"`).
func unquoteJSON(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var unquoted string
		if json.Unmarshal([]byte(s), &unquoted) == nil {
			return unquoted
		}
	}
	return s
}

func stringOption(name string, alias string, options url.Values) string {
	if len(options[name]) > 0 {
		return options[name][0]
	}
	if len(options[alias]) > 0 {
		return options[alias][0]
	}
	return ""
}

func durationOption(name string, unit, fallback time.Duration, options url.Values) time.Duration {
	v := intOption(name, -1, options)
	if v < 0 {
		return fallback
	}
	return time.Duration(v) * unit
}

// parseDocKeyRange extracts startkey/endkey/key/inclusive_end from URL params
// and sets StartKey, EndKey, ExclusiveEnd on the query.
func parseDocKeyRange(q *port.AllDocsQuery, opts url.Values) {
	q.StartKey = strings.ReplaceAll(stringOption("startkey", "start_key", opts), `"`, "")
	q.EndKey = strings.ReplaceAll(stringOption("endkey", "end_key", opts), `"`, "")
	if key := strings.ReplaceAll(stringOption("key", "key", opts), `"`, ""); key != "" {
		q.StartKey = key
		q.EndKey = key
	}
	if !boolOption("inclusive_end", true, opts) {
		q.ExclusiveEnd = true
	}
}

// parseViewQueryOptions parses all view query parameters from URL values
// into the AllDocsQuery struct. This includes pagination, key ranges
// (CBOR-encoded), reduce/group options, and doc ID filters.
func parseViewQueryOptions(q *port.AllDocsQuery, opts url.Values) {
	q.Skip = intOption("skip", 0, opts)
	q.Limit = intOption("limit", 100, opts)
	q.IncludeDocs = boolOption("include_docs", false, opts)
	q.ViewGroup = stringOption("group", "", opts)
	q.ViewGroupLevel = int(intOption("group_level", 0, opts))
	q.ViewDescending = boolOption("descending", false, opts)
	q.ViewUpdateSeq = boolOption("update_seq", false, opts)
	q.ViewOmitSortedInfo = opts.Get("sorted") == "false"

	if keysRaw := opts.Get("keys"); keysRaw != "" {
		var keys []interface{}
		if err := json.Unmarshal([]byte(keysRaw), &keys); err == nil {
			q.ViewKeys = keys
		}
	}

	q.ViewStartKey, q.ViewEndKey, q.ViewDecodedStartKey, q.ViewDecodedEndKey, q.ViewExclusiveEnd = viewKeyRange(opts)
	q.StartKeyDocID = opts.Get("startkey_docid")
	q.EndKeyDocID = opts.Get("endkey_docid")

	if q.ViewDescending && q.ViewEndKey != nil && !q.ViewExclusiveEnd {
		if len(q.ViewEndKey) >= 10 {
			q.ViewEndKey = q.ViewEndKey[:len(q.ViewEndKey)-10]
		}
	}
}
