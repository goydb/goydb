package index

import (
	"testing"

	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLuceneQuery(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		check   func(t *testing.T, q query.Query)
		wantErr bool
	}{
		{
			name:  "empty query → match all",
			input: "",
			check: func(t *testing.T, q query.Query) {
				_, ok := q.(*query.MatchAllQuery)
				assert.True(t, ok, "expected MatchAllQuery, got %T", q)
			},
		},
		{
			name:  "match all *:*",
			input: "*:*",
			check: func(t *testing.T, q query.Query) {
				_, ok := q.(*query.MatchAllQuery)
				assert.True(t, ok, "expected MatchAllQuery, got %T", q)
			},
		},
		{
			name:  "simple term",
			input: "hello",
			check: func(t *testing.T, q query.Query) {
				mq, ok := q.(*query.MatchQuery)
				require.True(t, ok, "expected MatchQuery, got %T", q)
				assert.Equal(t, "hello", mq.Match)
				assert.Empty(t, mq.FieldVal)
			},
		},
		{
			name:  "field:value",
			input: "language:en",
			check: func(t *testing.T, q query.Query) {
				mq, ok := q.(*query.MatchQuery)
				require.True(t, ok, "expected MatchQuery, got %T", q)
				assert.Equal(t, "en", mq.Match)
				assert.Equal(t, "language", mq.FieldVal)
			},
		},
		{
			name:  "quoted phrase",
			input: `"hello world"`,
			check: func(t *testing.T, q query.Query) {
				mq, ok := q.(*query.MatchPhraseQuery)
				require.True(t, ok, "expected MatchPhraseQuery, got %T", q)
				assert.Equal(t, "hello world", mq.MatchPhrase)
			},
		},
		{
			name:  "field:\"phrase\"",
			input: `title:"hello world"`,
			check: func(t *testing.T, q query.Query) {
				mq, ok := q.(*query.MatchPhraseQuery)
				require.True(t, ok, "expected MatchPhraseQuery, got %T", q)
				assert.Equal(t, "hello world", mq.MatchPhrase)
				assert.Equal(t, "title", mq.FieldVal)
			},
		},
		{
			name:  "wildcard term",
			input: "hel*",
			check: func(t *testing.T, q query.Query) {
				wq, ok := q.(*query.WildcardQuery)
				require.True(t, ok, "expected WildcardQuery, got %T", q)
				assert.Equal(t, "hel*", wq.Wildcard)
			},
		},
		{
			name:  "field wildcard",
			input: "name:hel*",
			check: func(t *testing.T, q query.Query) {
				wq, ok := q.(*query.WildcardQuery)
				require.True(t, ok, "expected WildcardQuery, got %T", q)
				assert.Equal(t, "hel*", wq.Wildcard)
				assert.Equal(t, "name", wq.FieldVal)
			},
		},

		// --- boolean operators -------------------------------------------------
		{
			name:  "explicit AND",
			input: "a AND b",
			check: func(t *testing.T, q query.Query) {
				cq, ok := q.(*query.ConjunctionQuery)
				require.True(t, ok, "expected ConjunctionQuery, got %T", q)
				assert.Len(t, cq.Conjuncts, 2)
			},
		},
		{
			name:  "explicit OR",
			input: "a OR b",
			check: func(t *testing.T, q query.Query) {
				dq, ok := q.(*query.DisjunctionQuery)
				require.True(t, ok, "expected DisjunctionQuery, got %T", q)
				assert.Len(t, dq.Disjuncts, 2)
			},
		},
		{
			name:  "NOT",
			input: "NOT a",
			check: func(t *testing.T, q query.Query) {
				bq, ok := q.(*query.BooleanQuery)
				require.True(t, ok, "expected BooleanQuery, got %T", q)
				assert.Nil(t, bq.Must)
				assert.NotNil(t, bq.MustNot)
			},
		},
		{
			name:  "implicit OR between adjacent terms",
			input: "hello world",
			check: func(t *testing.T, q query.Query) {
				dq, ok := q.(*query.DisjunctionQuery)
				require.True(t, ok, "expected DisjunctionQuery, got %T", q)
				assert.Len(t, dq.Disjuncts, 2)
			},
		},

		// --- precedence --------------------------------------------------------
		{
			name:  "AND binds tighter than implicit OR",
			input: "a b AND c",
			check: func(t *testing.T, q query.Query) {
				// Expect: DisjunctionQuery([a, ConjunctionQuery([b, c])])
				dq, ok := q.(*query.DisjunctionQuery)
				require.True(t, ok, "expected DisjunctionQuery, got %T", q)
				require.Len(t, dq.Disjuncts, 2)
				_, ok = dq.Disjuncts[0].(*query.MatchQuery)
				assert.True(t, ok, "first disjunct should be MatchQuery")
				cq, ok := dq.Disjuncts[1].(*query.ConjunctionQuery)
				require.True(t, ok, "second disjunct should be ConjunctionQuery")
				assert.Len(t, cq.Conjuncts, 2)
			},
		},
		{
			name:  "AND binds tighter than explicit OR",
			input: "a AND b OR c",
			check: func(t *testing.T, q query.Query) {
				// Expect: DisjunctionQuery([ConjunctionQuery([a, b]), c])
				dq, ok := q.(*query.DisjunctionQuery)
				require.True(t, ok, "expected DisjunctionQuery, got %T", q)
				require.Len(t, dq.Disjuncts, 2)
				_, ok = dq.Disjuncts[0].(*query.ConjunctionQuery)
				assert.True(t, ok, "first disjunct should be ConjunctionQuery")
			},
		},
		{
			name:  "parentheses override precedence",
			input: "(a OR b) AND c",
			check: func(t *testing.T, q query.Query) {
				cq, ok := q.(*query.ConjunctionQuery)
				require.True(t, ok, "expected ConjunctionQuery, got %T", q)
				require.Len(t, cq.Conjuncts, 2)
				dq, ok := cq.Conjuncts[0].(*query.DisjunctionQuery)
				require.True(t, ok, "first conjunct should be DisjunctionQuery")
				assert.Len(t, dq.Disjuncts, 2)
			},
		},

		// --- realistic CouchDB queries -----------------------------------------
		{
			name:  "user query: (noga) AND language:en",
			input: "(noga) AND language:en",
			check: func(t *testing.T, q query.Query) {
				cq, ok := q.(*query.ConjunctionQuery)
				require.True(t, ok, "expected ConjunctionQuery, got %T", q)
				require.Len(t, cq.Conjuncts, 2)
				mq, ok := cq.Conjuncts[0].(*query.MatchQuery)
				require.True(t, ok, "first conjunct should be MatchQuery")
				assert.Equal(t, "noga", mq.Match)
				fq, ok := cq.Conjuncts[1].(*query.MatchQuery)
				require.True(t, ok, "second conjunct should be MatchQuery")
				assert.Equal(t, "en", fq.Match)
				assert.Equal(t, "language", fq.FieldVal)
			},
		},
		{
			name:  "field AND NOT field",
			input: "language:en AND NOT status:draft",
			check: func(t *testing.T, q query.Query) {
				cq, ok := q.(*query.ConjunctionQuery)
				require.True(t, ok, "expected ConjunctionQuery, got %T", q)
				require.Len(t, cq.Conjuncts, 2)
				bq, ok := cq.Conjuncts[1].(*query.BooleanQuery)
				require.True(t, ok, "second conjunct should be BooleanQuery (NOT)")
				assert.NotNil(t, bq.MustNot)
			},
		},
		{
			name:  "triple AND",
			input: "a AND b AND c",
			check: func(t *testing.T, q query.Query) {
				cq, ok := q.(*query.ConjunctionQuery)
				require.True(t, ok, "expected ConjunctionQuery, got %T", q)
				assert.Len(t, cq.Conjuncts, 3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := parseLuceneQuery(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, q)
		})
	}
}
