//go:build !nosearch

package index

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/blevesearch/bleve/v2/search/query"
)

// parseLuceneQuery converts a CouchDB/Lucene-style query string to a Bleve
// query. Bleve's built-in QueryStringQuery does not support AND/OR/NOT keyword
// operators — it treats them as literal search terms. This parser implements
// the subset of Lucene query syntax that CouchDB search exposes:
//
//   - Boolean operators: AND, OR, NOT
//   - Parenthesised groups: (a OR b) AND c
//   - Field-scoped terms: field:value
//   - Quoted phrases: "exact phrase", field:"exact phrase"
//   - Wildcards: hel*, field:h?llo
//   - Match-all: *:*
//   - Implicit OR between adjacent terms: a b  ≡  a OR b
//
// Operator precedence (highest to lowest): NOT > AND > OR / implicit-OR.
func parseLuceneQuery(input string) (query.Query, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return query.NewMatchAllQuery(), nil
	}

	tokens := tokenizeLucene(input)
	p := &luceneParser{tokens: tokens}
	q, err := p.parseQuery()
	if err != nil {
		return nil, err
	}

	if p.peek().typ != ltEOF {
		return nil, fmt.Errorf("unexpected token at position %d: %q", p.pos, p.peek().val)
	}

	return q, nil
}

// ---------------------------------------------------------------------------
// Tokeniser
// ---------------------------------------------------------------------------

type luceneTokenType int

const (
	ltWord   luceneTokenType = iota // bare word or field:value
	ltPhrase                        // "quoted phrase"
	ltAND                           // AND
	ltOR                            // OR
	ltNOT                           // NOT
	ltLParen                        // (
	ltRParen                        // )
	ltEOF                           // end of input
)

type luceneToken struct {
	typ luceneTokenType
	val string
}

func tokenizeLucene(input string) []luceneToken {
	var tokens []luceneToken
	runes := []rune(input)
	i := 0

	for i < len(runes) {
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}

		switch runes[i] {
		case '(':
			tokens = append(tokens, luceneToken{typ: ltLParen, val: "("})
			i++
		case ')':
			tokens = append(tokens, luceneToken{typ: ltRParen, val: ")"})
			i++
		case '"':
			i++ // skip opening quote
			start := i
			for i < len(runes) && runes[i] != '"' {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++ // skip escaped char
				}
				i++
			}
			tokens = append(tokens, luceneToken{typ: ltPhrase, val: string(runes[start:i])})
			if i < len(runes) {
				i++ // skip closing quote
			}
		default:
			start := i
			for i < len(runes) && !unicode.IsSpace(runes[i]) &&
				runes[i] != '(' && runes[i] != ')' && runes[i] != '"' {
				i++
			}
			word := string(runes[start:i])
			switch word {
			case "AND":
				tokens = append(tokens, luceneToken{typ: ltAND, val: word})
			case "OR":
				tokens = append(tokens, luceneToken{typ: ltOR, val: word})
			case "NOT":
				tokens = append(tokens, luceneToken{typ: ltNOT, val: word})
			default:
				tokens = append(tokens, luceneToken{typ: ltWord, val: word})
			}
		}
	}

	tokens = append(tokens, luceneToken{typ: ltEOF})
	return tokens
}

// ---------------------------------------------------------------------------
// Recursive-descent parser
// ---------------------------------------------------------------------------

type luceneParser struct {
	tokens []luceneToken
	pos    int
}

func (p *luceneParser) peek() luceneToken {
	if p.pos >= len(p.tokens) {
		return luceneToken{typ: ltEOF}
	}
	return p.tokens[p.pos]
}

func (p *luceneParser) advance() luceneToken {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *luceneParser) match(typ luceneTokenType) bool {
	if p.peek().typ == typ {
		p.advance()
		return true
	}
	return false
}

// canStartClause returns true when the current token can begin a new clause.
func (p *luceneParser) canStartClause() bool {
	switch p.peek().typ {
	case ltWord, ltPhrase, ltLParen, ltNOT:
		return true
	}
	return false
}

// --- grammar rules ----------------------------------------------------------

func (p *luceneParser) parseQuery() (query.Query, error) {
	return p.parseOr()
}

// parseOr: and_expr ( ("OR" | <implicit>) and_expr )*
func (p *luceneParser) parseOr() (query.Query, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	disjuncts := []query.Query{left}

	for {
		if p.match(ltOR) {
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			disjuncts = append(disjuncts, right)
		} else if p.canStartClause() {
			// implicit OR — adjacent terms without an explicit operator
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			disjuncts = append(disjuncts, right)
		} else {
			break
		}
	}

	if len(disjuncts) == 1 {
		return disjuncts[0], nil
	}
	return query.NewDisjunctionQuery(disjuncts), nil
}

// parseAnd: not_expr ("AND" not_expr)*
func (p *luceneParser) parseAnd() (query.Query, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}

	conjuncts := []query.Query{left}

	for p.match(ltAND) {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		conjuncts = append(conjuncts, right)
	}

	if len(conjuncts) == 1 {
		return conjuncts[0], nil
	}
	return query.NewConjunctionQuery(conjuncts), nil
}

// parseNot: "NOT" not_expr | primary
func (p *luceneParser) parseNot() (query.Query, error) {
	if p.match(ltNOT) {
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		// BooleanQuery.Searcher automatically adds MatchAll when only
		// MustNot is present, so we don't need to add it ourselves.
		return query.NewBooleanQuery(nil, nil, []query.Query{inner}), nil
	}
	return p.parsePrimary()
}

// parsePrimary: "(" query ")" | phrase | field:"phrase" | field:term | term
func (p *luceneParser) parsePrimary() (query.Query, error) {
	// parenthesised group
	if p.match(ltLParen) {
		q, err := p.parseQuery()
		if err != nil {
			return nil, err
		}
		if !p.match(ltRParen) {
			return nil, fmt.Errorf("expected closing parenthesis")
		}
		return q, nil
	}

	// quoted phrase (without field prefix)
	if p.peek().typ == ltPhrase {
		t := p.advance()
		return query.NewMatchPhraseQuery(t.val), nil
	}

	// word — possibly field:value or field: "phrase"
	if p.peek().typ == ltWord {
		t := p.advance()

		// *:* → match all
		if t.val == "*:*" {
			return query.NewMatchAllQuery(), nil
		}

		// field: followed by a quoted phrase → field:"phrase"
		if strings.HasSuffix(t.val, ":") && p.peek().typ == ltPhrase {
			field := t.val[:len(t.val)-1]
			phrase := p.advance()
			mq := query.NewMatchPhraseQuery(phrase.val)
			mq.SetField(field)
			return mq, nil
		}

		// field:value within a single token
		if idx := strings.IndexByte(t.val, ':'); idx > 0 && idx < len(t.val)-1 {
			field := t.val[:idx]
			value := t.val[idx+1:]
			return makeFieldQuery(field, value), nil
		}

		// bare term
		return makeTermQuery(t.val), nil
	}

	return nil, fmt.Errorf("unexpected token: %q", p.peek().val)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeFieldQuery(field, value string) query.Query {
	if strings.ContainsAny(value, "*?") {
		q := query.NewWildcardQuery(value)
		q.SetField(field)
		return q
	}
	q := query.NewMatchQuery(value)
	q.SetField(field)
	return q
}

func makeTermQuery(term string) query.Query {
	if strings.ContainsAny(term, "*?") {
		return query.NewWildcardQuery(term)
	}
	return query.NewMatchQuery(term)
}
