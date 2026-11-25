package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The expression language of ${{ }} (spec LEG-482 §3). It stays a path, read
// by a tokenizer that understands quoted literals and brackets, which
// web/src/lib/workflowExpressions.ts mirrors; testdata/expressions.json is
// checked by both:
//
//	expression := operand ( "??" operand )*
//	operand    := path | literal          (the first is always a path)
//	path       := name ( "." name | "[" integer "]" )*
//	literal    := 'text' | "text" | null | true | false | whole number
//
// A name is a run of characters other than space, quotes, dots, brackets,
// braces and ?. [n] is the nth item of a list, [-1] the last. a ?? b is b
// when a is missing or null; the empty string counts as a value.

// PathPart is one step of a path: a name (.field) or an index ([n]).
type PathPart struct {
	Name    string
	Index   int
	IsIndex bool
}

// Operand is one side of ??: a path, or a literal value.
type Operand struct {
	Path      []PathPart
	IsLiteral bool
	Literal   any
}

// Expression is what ${{ }} holds: one or more operands joined by ??.
type Expression struct {
	Operands []Operand
}

// String writes a path back as the dotted form a file says it in.
func pathString(path []PathPart) string {
	var b strings.Builder
	for i, part := range path {
		if part.IsIndex {
			fmt.Fprintf(&b, "[%d]", part.Index)
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(part.Name)
	}
	return b.String()
}

// names are the leading names of a path, up to its first index.
func pathNames(path []PathPart) []string {
	var names []string
	for _, part := range path {
		if part.IsIndex {
			break
		}
		names = append(names, part.Name)
	}
	return names
}

func nameChar(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '.', '[', ']', '{', '}', '?', '\'', '"':
		return false
	}
	return true
}

// ParseExpression reads the inside of a ${{ }}.
func ParseExpression(text string) (*Expression, error) {
	p := exprParser{text: text}
	expression := &Expression{}
	for {
		p.space()
		operand, err := p.operand(len(expression.Operands) == 0)
		if err != nil {
			return nil, err
		}
		expression.Operands = append(expression.Operands, operand)
		p.space()
		if p.done() {
			return expression, nil
		}
		if !strings.HasPrefix(p.text[p.at:], "??") {
			return nil, fmt.Errorf("unexpected %q in %q: join a fallback with ??", p.text[p.at:], text)
		}
		p.at += 2
	}
}

// ParsePath reads a bare path, such as a condition's field.
func ParsePath(text string) ([]PathPart, error) {
	p := exprParser{text: text}
	p.space()
	operand, err := p.operand(true)
	if err != nil {
		return nil, err
	}
	p.space()
	if !p.done() {
		return nil, fmt.Errorf("%q is not a path: a field takes a path alone", text)
	}
	return operand.Path, nil
}

type exprParser struct {
	text string
	at   int
}

func (p *exprParser) done() bool { return p.at >= len(p.text) }

func (p *exprParser) space() {
	for !p.done() && strings.IndexByte(" \t\n\r", p.text[p.at]) >= 0 {
		p.at++
	}
}

func (p *exprParser) operand(first bool) (Operand, error) {
	if p.done() {
		if first {
			return Operand{}, fmt.Errorf("empty expression")
		}
		return Operand{}, fmt.Errorf("?? needs a fallback after it")
	}
	c := p.text[p.at]
	if c == '\'' || c == '"' {
		end := strings.IndexByte(p.text[p.at+1:], c)
		if end < 0 {
			return Operand{}, fmt.Errorf("unterminated %c in %q", c, p.text)
		}
		value := p.text[p.at+1 : p.at+1+end]
		p.at += end + 2
		if first {
			return Operand{}, fmt.Errorf("an expression starts with a path, not the literal %q", value)
		}
		return Operand{IsLiteral: true, Literal: value}, nil
	}
	start := p.at
	for !p.done() && nameChar(p.text[p.at]) {
		p.at++
	}
	word := p.text[start:p.at]
	if word == "" {
		return Operand{}, fmt.Errorf("unexpected %q in %q", p.text[p.at:], p.text)
	}
	if literal, ok := wordLiteral(word); ok {
		if first {
			return Operand{}, fmt.Errorf("an expression starts with a path, not the literal %s", word)
		}
		return Operand{IsLiteral: true, Literal: literal}, nil
	}
	path := []PathPart{{Name: word}}
	for !p.done() {
		switch p.text[p.at] {
		case '.':
			p.at++
			start := p.at
			for !p.done() && nameChar(p.text[p.at]) {
				p.at++
			}
			if start == p.at {
				return Operand{}, fmt.Errorf("a name must follow . in %q", p.text)
			}
			path = append(path, PathPart{Name: p.text[start:p.at]})
		case '[':
			end := strings.IndexByte(p.text[p.at:], ']')
			if end < 0 {
				return Operand{}, fmt.Errorf("unclosed [ in %q", p.text)
			}
			inside := strings.TrimSpace(p.text[p.at+1 : p.at+end])
			number, ok := wordLiteral(inside)
			index, isNumber := number.(int64)
			if !ok || !isNumber {
				return Operand{}, fmt.Errorf("[%s] in %q: an index is a whole number, such as [0] or [-1]", p.text[p.at+1:p.at+end], p.text)
			}
			path = append(path, PathPart{Index: int(index), IsIndex: true})
			p.at += end + 1
		default:
			return Operand{Path: path}, nil
		}
	}
	return Operand{Path: path}, nil
}

// wordLiteral reads null, true, false or a whole number.
func wordLiteral(word string) (any, bool) {
	switch word {
	case "null":
		return nil, true
	case "true":
		return true, true
	case "false":
		return false, true
	}
	digits := strings.TrimPrefix(word, "-")
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return nil, false
	}
	n, err := strconv.ParseInt(word, 10, 64)
	return n, err == nil
}

// exprMatch is one ${{ }} in a text: where it starts and ends (the braces
// included), and its inside.
type exprMatch struct {
	start, end int
	inner      string
}

// findExpressions lists each ${{ }} in text. A quoted literal may hold }}.
// A ${{ with no }} after it is text.
func findExpressions(text string) []exprMatch {
	var out []exprMatch
	from := 0
	for {
		open := strings.Index(text[from:], "${{")
		if open < 0 {
			return out
		}
		start := from + open
		at := start + 3
		end := -1
		for at < len(text) {
			c := text[at]
			if c == '\'' || c == '"' {
				// A quote with no close is not a literal: the parse
				// says so.
				if close := strings.IndexByte(text[at+1:], c); close >= 0 {
					at += close + 2
					continue
				}
			}
			if strings.HasPrefix(text[at:], "}}") {
				end = at + 2
				break
			}
			at++
		}
		if end < 0 {
			return out
		}
		out = append(out, exprMatch{start: start, end: end, inner: strings.TrimSpace(text[start+3 : end-2])})
		from = end
	}
}

// HasExpression reports whether text holds a ${{ }}.
func HasExpression(text string) bool { return len(findExpressions(text)) > 0 }

// wholeExpression is the inside of text's ${{ }} when it is all of text.
func wholeExpression(text string) (string, bool) {
	matches := findExpressions(text)
	if len(matches) != 1 || matches[0].start != 0 || matches[0].end != len(text) {
		return "", false
	}
	return matches[0].inner, true
}

// errMissing is a path that reads nothing: an input not given, a skipped
// step's output, a field or an item that is not there.
var errMissing = errors.New("missing")

// reach follows the rest of a path into a value: .field into an object,
// [n] into a list. ok is false when something on the way is not there.
func reach(value any, rest []PathPart) (any, bool) {
	for _, part := range rest {
		if part.IsIndex {
			list, ok := value.([]any)
			if !ok {
				return nil, false
			}
			index := part.Index
			if index < 0 {
				index += len(list)
			}
			if index < 0 || index >= len(list) {
				return nil, false
			}
			value = list[index]
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part.Name]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// numberOf is a value as a number, when it is one.
func numberOf(value any) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// ValuesEqual compares two values as a condition does: numbers by value
// (3 equals 3.0 after JSON), objects and lists item by item.
func ValuesEqual(a, b any) bool {
	if x, ok := numberOf(a); ok {
		y, ok := numberOf(b)
		return ok && x == y
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !ValuesEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, ok := y[key]
			if !ok || !ValuesEqual(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

// compareValues orders two numbers by value, or two strings as text; ok is
// false for anything else.
func compareValues(a, b any) (int, bool) {
	if x, ok := numberOf(a); ok {
		y, ok := numberOf(b)
		if !ok || math.IsNaN(x) || math.IsNaN(y) {
			return 0, false
		}
		switch {
		case x < y:
			return -1, true
		case x > y:
			return 1, true
		}
		return 0, true
	}
	x, ok := a.(string)
	y, ok2 := b.(string)
	if !ok || !ok2 {
		return 0, false
	}
	return strings.Compare(x, y), true
}

// isEmpty reports whether a value is empty: null, "", an empty list or
// object.
func isEmpty(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}
