package handler

import (
	"net/url"
	"testing"

	"github.com/goydb/goydb/pkg/port"
)

func TestIntOption(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		fallback int64
		opts     url.Values
		want     int64
	}{
		{"default fallback", "skip", 42, url.Values{}, 42},
		{"valid value", "skip", 0, url.Values{"skip": {"10"}}, 10},
		{"invalid string", "skip", 5, url.Values{"skip": {"abc"}}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intOption(tt.key, tt.fallback, tt.opts)
			if got != tt.want {
				t.Errorf("intOption(%q) = %d, want %d", tt.key, got, tt.want)
			}
		})
	}
}

func TestBoolOption(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		fallback bool
		opts     url.Values
		want     bool
	}{
		{"default fallback", "x", false, url.Values{}, false},
		{"true", "x", false, url.Values{"x": {"true"}}, true},
		{"false", "x", true, url.Values{"x": {"false"}}, false},
		{"empty string returns fallback", "x", true, url.Values{"x": {""}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boolOption(tt.key, tt.fallback, tt.opts)
			if got != tt.want {
				t.Errorf("boolOption(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestStringOption(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		alias string
		opts  url.Values
		want  string
	}{
		{"primary name", "startkey", "start_key", url.Values{"startkey": {"a"}}, "a"},
		{"alias fallback", "startkey", "start_key", url.Values{"start_key": {"b"}}, "b"},
		{"primary wins", "startkey", "start_key", url.Values{"startkey": {"a"}, "start_key": {"b"}}, "a"},
		{"missing", "startkey", "start_key", url.Values{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringOption(tt.key, tt.alias, tt.opts)
			if got != tt.want {
				t.Errorf("stringOption(%q, %q) = %q, want %q", tt.key, tt.alias, got, tt.want)
			}
		})
	}
}

func TestParseDocKeyRange(t *testing.T) {
	tests := []struct {
		name         string
		opts         url.Values
		wantStart    string
		wantEnd      string
		wantExcEnd   bool
	}{
		{
			"startkey and endkey",
			url.Values{"startkey": {`"a"`}, "endkey": {`"z"`}},
			"a", "z", false,
		},
		{
			"key shorthand sets both",
			url.Values{"key": {`"m"`}},
			"m", "m", false,
		},
		{
			"inclusive_end=false",
			url.Values{"startkey": {`"a"`}, "endkey": {`"z"`}, "inclusive_end": {"false"}},
			"a", "z", true,
		},
		{
			"alias start_key and end_key",
			url.Values{"start_key": {`"x"`}, "end_key": {`"y"`}},
			"x", "y", false,
		},
		{
			"empty values",
			url.Values{},
			"", "", false,
		},
		{
			"key overrides startkey/endkey",
			url.Values{"startkey": {`"a"`}, "endkey": {`"z"`}, "key": {`"m"`}},
			"m", "m", false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var q port.AllDocsQuery
			parseDocKeyRange(&q, tt.opts)
			if q.StartKey != tt.wantStart {
				t.Errorf("StartKey = %q, want %q", q.StartKey, tt.wantStart)
			}
			if q.EndKey != tt.wantEnd {
				t.Errorf("EndKey = %q, want %q", q.EndKey, tt.wantEnd)
			}
			if q.ExclusiveEnd != tt.wantExcEnd {
				t.Errorf("ExclusiveEnd = %v, want %v", q.ExclusiveEnd, tt.wantExcEnd)
			}
		})
	}
}

func TestAllDocsQueryParamsToValues(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }

	tests := []struct {
		name       string
		qp         allDocsQueryParams
		wantStart  string
		wantEnd    string
		wantExcEnd bool
	}{
		{
			"startkey and endkey",
			allDocsQueryParams{StartKey: `"a"`, EndKey: `"z"`},
			"a", "z", false,
		},
		{
			"start_key alias",
			allDocsQueryParams{StartKeyAlt: `"x"`, EndKeyAlt: `"y"`},
			"x", "y", false,
		},
		{
			"startkey wins over start_key",
			allDocsQueryParams{StartKey: `"a"`, StartKeyAlt: `"b"`},
			"a", "", false,
		},
		{
			"key shorthand sets both",
			allDocsQueryParams{Key: `"m"`},
			"m", "m", false,
		},
		{
			"key overrides startkey/endkey",
			allDocsQueryParams{StartKey: `"a"`, EndKey: `"z"`, Key: `"m"`},
			"m", "m", false,
		},
		{
			"inclusive_end false",
			allDocsQueryParams{StartKey: `"a"`, EndKey: `"z"`, InclusiveEnd: boolPtr(false)},
			"a", "z", true,
		},
		{
			"inclusive_end true (default behavior)",
			allDocsQueryParams{StartKey: `"a"`, EndKey: `"z"`, InclusiveEnd: boolPtr(true)},
			"a", "z", false,
		},
		{
			"inclusive_end nil (default behavior)",
			allDocsQueryParams{StartKey: `"a"`, EndKey: `"z"`},
			"a", "z", false,
		},
		{
			"empty params",
			allDocsQueryParams{},
			"", "", false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.qp.toValues()
			var q port.AllDocsQuery
			parseDocKeyRange(&q, opts)
			if q.StartKey != tt.wantStart {
				t.Errorf("StartKey = %q, want %q", q.StartKey, tt.wantStart)
			}
			if q.EndKey != tt.wantEnd {
				t.Errorf("EndKey = %q, want %q", q.EndKey, tt.wantEnd)
			}
			if q.ExclusiveEnd != tt.wantExcEnd {
				t.Errorf("ExclusiveEnd = %v, want %v", q.ExclusiveEnd, tt.wantExcEnd)
			}
		})
	}
}

func TestParseViewQueryOptions(t *testing.T) {
	t.Run("skip and limit", func(t *testing.T) {
		opts := url.Values{"skip": {"5"}, "limit": {"25"}}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if q.Skip != 5 {
			t.Errorf("Skip = %d, want 5", q.Skip)
		}
		if q.Limit != 25 {
			t.Errorf("Limit = %d, want 25", q.Limit)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, url.Values{})
		if q.Skip != 0 {
			t.Errorf("Skip = %d, want 0", q.Skip)
		}
		if q.Limit != 100 {
			t.Errorf("Limit = %d, want 100", q.Limit)
		}
		if q.IncludeDocs {
			t.Error("IncludeDocs should default to false")
		}
		if q.ViewDescending {
			t.Error("ViewDescending should default to false")
		}
		if q.ViewUpdateSeq {
			t.Error("ViewUpdateSeq should default to false")
		}
		if q.ViewOmitSortedInfo {
			t.Error("ViewOmitSortedInfo should default to false")
		}
	})

	t.Run("descending and group", func(t *testing.T) {
		opts := url.Values{
			"descending":  {"true"},
			"group":       {"true"},
			"group_level": {"2"},
		}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if !q.ViewDescending {
			t.Error("ViewDescending should be true")
		}
		if q.ViewGroup != "true" {
			t.Errorf("ViewGroup = %q, want %q", q.ViewGroup, "true")
		}
		if q.ViewGroupLevel != 2 {
			t.Errorf("ViewGroupLevel = %d, want 2", q.ViewGroupLevel)
		}
	})

	t.Run("keys JSON array", func(t *testing.T) {
		opts := url.Values{"keys": {`["a","b","c"]`}}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if len(q.ViewKeys) != 3 {
			t.Fatalf("ViewKeys length = %d, want 3", len(q.ViewKeys))
		}
		if q.ViewKeys[0] != "a" || q.ViewKeys[1] != "b" || q.ViewKeys[2] != "c" {
			t.Errorf("ViewKeys = %v, want [a b c]", q.ViewKeys)
		}
	})

	t.Run("startkey_docid and endkey_docid", func(t *testing.T) {
		opts := url.Values{
			"startkey_docid": {"doc1"},
			"endkey_docid":   {"doc9"},
		}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if q.StartKeyDocID != "doc1" {
			t.Errorf("StartKeyDocID = %q, want %q", q.StartKeyDocID, "doc1")
		}
		if q.EndKeyDocID != "doc9" {
			t.Errorf("EndKeyDocID = %q, want %q", q.EndKeyDocID, "doc9")
		}
	})

	t.Run("sorted=false", func(t *testing.T) {
		opts := url.Values{"sorted": {"false"}}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if !q.ViewOmitSortedInfo {
			t.Error("ViewOmitSortedInfo should be true when sorted=false")
		}
	})

	t.Run("update_seq=true", func(t *testing.T) {
		opts := url.Values{"update_seq": {"true"}}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if !q.ViewUpdateSeq {
			t.Error("ViewUpdateSeq should be true")
		}
	})

	t.Run("include_docs=true", func(t *testing.T) {
		opts := url.Values{"include_docs": {"true"}}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if !q.IncludeDocs {
			t.Error("IncludeDocs should be true")
		}
	})

	t.Run("startkey endkey CBOR range", func(t *testing.T) {
		opts := url.Values{"startkey": {`"alpha"`}, "endkey": {`"omega"`}}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		if q.ViewStartKey == nil {
			t.Error("ViewStartKey should not be nil")
		}
		if q.ViewEndKey == nil {
			t.Error("ViewEndKey should not be nil")
		}
		if q.ViewDecodedStartKey != "alpha" {
			t.Errorf("ViewDecodedStartKey = %v, want %q", q.ViewDecodedStartKey, "alpha")
		}
		if q.ViewDecodedEndKey != "omega" {
			t.Errorf("ViewDecodedEndKey = %v, want %q", q.ViewDecodedEndKey, "omega")
		}
	})

	t.Run("descending strips endkey padding", func(t *testing.T) {
		opts := url.Values{
			"descending": {"true"},
			"startkey":   {`"z"`},
			"endkey":     {`"a"`},
		}
		var q port.AllDocsQuery
		parseViewQueryOptions(&q, opts)
		// viewKeyRange adds 10 bytes of 0xFF padding for inclusive endkey.
		// In descending mode, parseViewQueryOptions strips those 10 bytes.
		// So ViewEndKey should be the bare CBOR encoding of "a".
		if q.ViewEndKey == nil {
			t.Fatal("ViewEndKey should not be nil")
		}
		// Check that no 0xFF padding remains at the end.
		ek := q.ViewEndKey
		if len(ek) > 0 && ek[len(ek)-1] == 0xFF {
			t.Error("ViewEndKey should not end with 0xFF padding in descending mode")
		}
	})
}
