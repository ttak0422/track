// Package query implements track's tiny query language over indexed notes: a TABLE of note fields
// (title, tags, property keys) filtered by tag and typed property comparisons. One evaluator serves
// every surface — the `track query` CLI, embedded ```track-query fences in the live web workspace,
// and the static-site export — so a query means the same thing everywhere. The grammar (ADR 0033):
//
//	query = "TABLE" key ("," key)*
//	        [ "FROM" "#"tag ]
//	        [ "WHERE" cond ("AND" cond)* ]
//	        [ "SORT" key ["DESC"] ]
//	        [ "LIMIT" n ]
//	cond  = "#"tag | key op value | key        (bare key = presence check)
//	key   = attr | "props." name               (attr = a note attribute: title, tags)
//	op    = "=" | "!=" | "<" | ">"
//	value = "quoted string" | bareword
//
// Two namespaces, kept apart so they can never collide: a bare identifier is a note-intrinsic
// attribute (the noteAttrs set, which may grow), and props.<name> is the only way to read a user
// property (a sidecar prop or inline field). An unknown bare key is an ERROR, never a silent empty
// result — these queries are mostly written by agents, which can't see a silently-wrong answer.
//
// Keywords are uppercase, so a lowercase word is always a key or value. Tags are hierarchical:
// #a matches #a and #a/b but not #ab.
package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Query is one parsed query.
type Query struct {
	Columns []string
	From    string // tag filter (without '#'); "" selects every note
	Where   []Cond
	Sort    string // sort key; "" keeps the source order (most recently updated first)
	Desc    bool
	Limit   int // 0 = unlimited
}

// Cond is one WHERE condition: a #tag filter (Tag set), a comparison (Key+Op+Value), or a presence
// check (Key only).
type Cond struct {
	Tag   string
	Key   string
	Op    string // "=", "!=", "<", ">"; "" with Key set means presence
	Value string
}

// Parse parses a query expression. Errors carry the offending token so a typo is locatable.
func Parse(input string) (Query, error) {
	toks, err := lex(input)
	if err != nil {
		return Query{}, err
	}
	p := &parser{toks: toks}

	var q Query
	if !p.eat("TABLE") {
		return Query{}, fmt.Errorf("query must start with TABLE, got %q", p.peek())
	}
	for {
		col, err := p.key("column")
		if err != nil {
			return Query{}, err
		}
		q.Columns = append(q.Columns, col)
		if !p.eat(",") {
			break
		}
	}
	if p.eat("FROM") {
		tag, err := p.tag()
		if err != nil {
			return Query{}, err
		}
		q.From = tag
	}
	if p.eat("WHERE") {
		for {
			cond, err := p.cond()
			if err != nil {
				return Query{}, err
			}
			q.Where = append(q.Where, cond)
			if !p.eat("AND") {
				break
			}
		}
	}
	if p.eat("SORT") {
		key, err := p.key("sort key")
		if err != nil {
			return Query{}, err
		}
		q.Sort = key
		q.Desc = p.eat("DESC")
	}
	if p.eat("LIMIT") {
		raw := p.next()
		n, err := strconv.Atoi(raw.text)
		if err != nil || n < 0 {
			return Query{}, fmt.Errorf("LIMIT needs a non-negative number, got %q", raw.text)
		}
		q.Limit = n
	}
	if !p.done() {
		return Query{}, fmt.Errorf("unexpected %q", p.peek())
	}
	return q, nil
}

type token struct {
	text   string
	quoted bool // a "quoted string": always a value, never a keyword or operator
}

// lex splits input into tokens: double-quoted strings (no escapes — a value with a quote in it is not
// worth a grammar), the punctuation , = != < >, and bare words (any other run of non-space text, so
// dates, numbers, #a/b tags, and key-names all lex as one word).
func lex(input string) ([]token, error) {
	var out []token
	i := 0
	for i < len(input) {
		c := input[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			end := strings.IndexByte(input[i+1:], '"')
			if end < 0 {
				return nil, fmt.Errorf("unterminated string at %q", input[i:])
			}
			out = append(out, token{text: input[i+1 : i+1+end], quoted: true})
			i += end + 2
		case c == ',' || c == '=' || c == '<' || c == '>':
			out = append(out, token{text: string(c)})
			i++
		case c == '!':
			if i+1 >= len(input) || input[i+1] != '=' {
				return nil, fmt.Errorf("unexpected %q (did you mean !=?)", input[i:])
			}
			out = append(out, token{text: "!="})
			i += 2
		default:
			start := i
			for i < len(input) && !strings.ContainsRune(" \t\n\r\",=<>!", rune(input[i])) {
				i++
			}
			out = append(out, token{text: input[start:i]})
		}
	}
	return out, nil
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) done() bool { return p.pos >= len(p.toks) }

func (p *parser) peek() string {
	if p.done() {
		return "end of query"
	}
	return p.toks[p.pos].text
}

func (p *parser) next() token {
	if p.done() {
		return token{}
	}
	t := p.toks[p.pos]
	p.pos++
	return t
}

// eat consumes the next token when it is exactly text (an unquoted keyword or punctuation).
func (p *parser) eat(text string) bool {
	if p.done() || p.toks[p.pos].quoted || p.toks[p.pos].text != text {
		return false
	}
	p.pos++
	return true
}

// key reads a column/sort/condition key: a bare word that is not punctuation or a #tag, and that
// checkKey accepts as either a note attribute or a props.<name> reference.
func (p *parser) key(what string) (string, error) {
	if p.done() {
		return "", fmt.Errorf("missing %s", what)
	}
	t := p.next()
	if t.text == "" || strings.HasPrefix(t.text, "#") || strings.ContainsAny(t.text, ",=<>") {
		return "", fmt.Errorf("expected a %s, got %q", what, t.text)
	}
	if err := checkKey(t.text); err != nil {
		return "", err
	}
	return t.text, nil
}

// noteAttrs are the bare identifiers that name a note-intrinsic attribute. Every other bare
// identifier is rejected, so a user property (reached only as props.<name>) can never be shadowed by
// an attribute, and a future attribute added here can never collide with an existing property.
var noteAttrs = []string{"title", "tags"}

// propName reports whether raw references a user property (props.<name>) and returns that name.
func propName(raw string) (name string, ok bool) {
	return strings.CutPrefix(raw, "props.")
}

// displayKey is a key as shown to a reader: a property drops its props. scope (props.status →
// status), since the prefix disambiguates the query, not the rendered header or lane label. A note
// attribute is already its own display name.
func displayKey(key string) string {
	if name, ok := propName(key); ok {
		return name
	}
	return key
}

// checkKey validates a column/sort/condition key. props.<name> names a user property; a bare word
// must be a note attribute. Anything else is a loud error rather than a silent empty result.
func checkKey(raw string) error {
	if name, ok := propName(raw); ok {
		if name == "" {
			return fmt.Errorf(`props. needs a property name, e.g. props.status`)
		}
		return nil
	}
	for _, a := range noteAttrs {
		if raw == a {
			return nil
		}
	}
	return fmt.Errorf("unknown key %q: note attributes are %s; query a property as props.%s",
		raw, strings.Join(noteAttrs, ", "), raw)
}

// tag reads a #tag token and returns the tag without the '#'.
func (p *parser) tag() (string, error) {
	if p.done() {
		return "", fmt.Errorf("missing #tag")
	}
	t := p.next()
	name := strings.TrimPrefix(t.text, "#")
	if t.quoted || !strings.HasPrefix(t.text, "#") || name == "" {
		return "", fmt.Errorf("expected #tag, got %q", t.text)
	}
	return name, nil
}

// cond reads one WHERE condition: #tag, key op value, or a bare key (presence).
func (p *parser) cond() (Cond, error) {
	if p.done() {
		return Cond{}, fmt.Errorf("missing condition after WHERE/AND")
	}
	if strings.HasPrefix(p.peek(), "#") && !p.toks[p.pos].quoted {
		tag, err := p.tag()
		if err != nil {
			return Cond{}, err
		}
		return Cond{Tag: tag}, nil
	}
	key, err := p.key("condition key")
	if err != nil {
		return Cond{}, err
	}
	op := ""
	switch p.peek() {
	case "=", "!=", "<", ">":
		op = p.next().text
	default:
		return Cond{Key: key}, nil // presence check
	}
	if p.done() {
		return Cond{}, fmt.Errorf("missing value after %s %s", key, op)
	}
	val := p.next()
	if !val.quoted && (strings.ContainsAny(val.text, ",=<>") || isKeyword(val.text)) {
		return Cond{}, fmt.Errorf("expected a value after %s %s, got %q", key, op, val.text)
	}
	return Cond{Key: key, Op: op, Value: val.text}, nil
}

func isKeyword(s string) bool {
	switch s {
	case "TABLE", "FROM", "WHERE", "AND", "SORT", "DESC", "LIMIT":
		return true
	}
	return false
}
