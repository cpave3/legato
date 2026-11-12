package config

import (
	"bytes"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Re-encoding a whole document through yaml.v3 keeps its comments and
// order but not all of its formatting: a flow mapping written
// "{ a: 1 }" comes back "{a: 1}", on every line of the file. So when an
// edit only changes scalar values in place, or adds entries to the end of
// block lists, the writer changes just that text of the file
// (patchDocument); anything else (a key added or removed, an entry
// removed) is written by re-encoding the document, as before.

// textPatch replaces old runes of one line from col (both counted from 0),
// or, with insert, puts lines after line.
type textPatch struct {
	line, col, old int
	text           string
	insert         bool
	lines          []string
}

// patchDocument is the original text with the changes from before to after
// made in place; ok is false when they cannot be.
func patchDocument(orig []byte, before, after *yaml.Node) ([]byte, bool) {
	if len(bytes.TrimSpace(orig)) == 0 {
		return nil, false
	}
	lines := strings.Split(string(orig), "\n")
	var patches []textPatch
	if !diffNodes(lines, before, after, &patches) {
		return nil, false
	}
	sort.SliceStable(patches, func(i, j int) bool {
		if patches[i].line != patches[j].line {
			return patches[i].line > patches[j].line
		}
		return patches[i].col > patches[j].col
	})
	for _, p := range patches {
		if p.insert {
			rest := append(append([]string{}, p.lines...), lines[p.line+1:]...)
			lines = append(lines[:p.line+1], rest...)
			continue
		}
		r := []rune(lines[p.line])
		lines[p.line] = string(r[:p.col]) + p.text + string(r[p.col+p.old:])
	}
	return []byte(strings.Join(lines, "\n")), true
}

func diffNodes(lines []string, b, a *yaml.Node, patches *[]textPatch) bool {
	if b.Kind != a.Kind {
		return false
	}
	switch b.Kind {
	case yaml.DocumentNode:
		if len(b.Content) != len(a.Content) {
			return false
		}
		for i := range b.Content {
			if !diffNodes(lines, b.Content[i], a.Content[i], patches) {
				return false
			}
		}
		return true
	case yaml.ScalarNode:
		if b.Value == a.Value && b.ShortTag() == a.ShortTag() {
			return true
		}
		return patchScalar(lines, b, a, patches)
	case yaml.MappingNode:
		if len(b.Content) != len(a.Content) {
			return false
		}
		for i := 0; i+1 < len(b.Content); i += 2 {
			if b.Content[i].Value != a.Content[i].Value || !diffNodes(lines, b.Content[i+1], a.Content[i+1], patches) {
				return false
			}
		}
		return true
	case yaml.SequenceNode:
		if len(a.Content) < len(b.Content) {
			return false
		}
		for i := range b.Content {
			if !diffNodes(lines, b.Content[i], a.Content[i], patches) {
				return false
			}
		}
		if len(a.Content) > len(b.Content) {
			return patchAppend(lines, b, a.Content[len(b.Content):], patches)
		}
		return true
	case yaml.AliasNode:
		return b.Value == a.Value
	}
	return false
}

// patchScalar replaces a single-line scalar's text with the new value's,
// written in the old one's quoting where it is a string.
func patchScalar(lines []string, b, a *yaml.Node, patches *[]textPatch) bool {
	if b.Line < 1 || b.Line > len(lines) || b.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.Contains(b.Value, "\n") {
		return false
	}
	line := []rune(lines[b.Line-1])
	col := b.Column - 1
	if col < 0 || col >= len(line) {
		return false
	}
	old := scalarLength(line[col:], b)
	if old <= 0 {
		return false
	}
	v := *a
	v.HeadComment, v.LineComment, v.FootComment = "", "", ""
	if v.ShortTag() == "!!str" && b.ShortTag() == "!!str" {
		v.Style = b.Style
	}
	out, err := yaml.Marshal(&v)
	if err != nil {
		return false
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" || strings.Contains(text, "\n") {
		return false
	}
	*patches = append(*patches, textPatch{line: b.Line - 1, col: col, old: old, text: text})
	return true
}

// scalarLength is how many runes the scalar b takes at the start of s, or 0
// when s does not start with it.
func scalarLength(s []rune, b *yaml.Node) int {
	switch {
	case b.Style&yaml.DoubleQuotedStyle != 0:
		if len(s) == 0 || s[0] != '"' {
			return 0
		}
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
	case b.Style&yaml.SingleQuotedStyle != 0:
		if len(s) == 0 || s[0] != '\'' {
			return 0
		}
		for i := 1; i < len(s); i++ {
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				return i + 1
			}
		}
	default:
		v := []rune(b.Value)
		if len(s) >= len(v) && string(s[:len(v)]) == b.Value {
			return len(v)
		}
	}
	return 0
}

// patchAppend adds entries after the last of a block list, each written as
// its siblings are: on one line in flow style when the last one is, else
// as a block, at the last one's indent.
func patchAppend(lines []string, b *yaml.Node, added []*yaml.Node, patches *[]textPatch) bool {
	if b.Style&yaml.FlowStyle != 0 || len(b.Content) == 0 {
		return false
	}
	last := b.Content[len(b.Content)-1]
	end, ok := lastLine(last)
	if !ok || last.Line < 1 || end > len(lines) {
		return false
	}
	start := []rune(lines[last.Line-1])
	if last.Column-1 > len(start) {
		return false
	}
	prefix := string(start[:last.Column-1])
	if !strings.HasSuffix(strings.TrimRight(prefix, " "), "-") || strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(prefix, " "), "-")) != "" {
		return false
	}
	flow := last.Style&yaml.FlowStyle != 0 || last.Kind == yaml.ScalarNode
	var out []string
	for _, item := range added {
		v := *item
		if flow && v.Kind != yaml.ScalarNode {
			v.Style |= yaml.FlowStyle
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(&v); err != nil {
			return false
		}
		enc.Close()
		text := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
		if flow && len(text) != 1 {
			return false
		}
		indent := strings.Repeat(" ", len([]rune(prefix)))
		for i, l := range text {
			if i == 0 {
				out = append(out, prefix+l)
			} else {
				out = append(out, indent+l)
			}
		}
	}
	*patches = append(*patches, textPatch{line: end - 1, insert: true, lines: out})
	return true
}

// lastLine is the last line a node's text is on, as far as its children
// tell; false for a block scalar, whose end they do not tell.
func lastLine(n *yaml.Node) (int, bool) {
	if n.Kind == yaml.ScalarNode && (n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.Contains(n.Value, "\n")) {
		return 0, false
	}
	end := n.Line
	for _, c := range n.Content {
		l, ok := lastLine(c)
		if !ok {
			return 0, false
		}
		if l > end {
			end = l
		}
	}
	return end, true
}
