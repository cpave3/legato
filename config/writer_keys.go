package config

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// A key names a place in config.yaml: sections joined by dots, as
// agents.sandbox.enabled, and list entries in brackets after a list's name:
// [2] by index (from 0), [name=github] the one entry whose field has that
// value (case ignored), and, to set, [+] a new entry at the end. So
// credentials[name=github].policy[0].operation is the operation of the
// first rule of the credential named github.

type keySelector struct {
	index         int
	field, value  string
	append, match bool
}

type keySegment struct {
	name      string
	selectors []keySelector
}

func parseKey(key string) ([]keySegment, error) {
	var segs []keySegment
	var parts []string
	depth, start := 0, 0
	for i, r := range key {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
		case '.':
			if depth == 0 {
				parts = append(parts, key[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, key[start:])
	if depth != 0 {
		return nil, fmt.Errorf("%s: a [ is unclosed", key)
	}
	for _, part := range parts {
		name, rest, _ := strings.Cut(part, "[")
		if name == "" {
			return nil, fmt.Errorf("%s: a part with no name", key)
		}
		seg := keySegment{name: name}
		if rest != "" {
			rest = "[" + rest
		}
		for rest != "" {
			if rest[0] != '[' {
				return nil, fmt.Errorf("%s: %q after a ]", key, rest)
			}
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, fmt.Errorf("%s: a [ is unclosed", key)
			}
			inner := strings.TrimSpace(rest[1:end])
			rest = rest[end+1:]
			var sel keySelector
			switch {
			case inner == "+":
				sel.append = true
			case strings.Contains(inner, "="):
				field, value, _ := strings.Cut(inner, "=")
				sel.match, sel.field, sel.value = true, strings.TrimSpace(field), strings.TrimSpace(value)
				if sel.field == "" {
					return nil, fmt.Errorf("%s: [%s] needs field=value", key, inner)
				}
			default:
				n, err := strconv.Atoi(inner)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("%s: [%s] is neither an index, field=value nor +", key, inner)
				}
				sel.index = n
			}
			seg.selectors = append(seg.selectors, sel)
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

// PlainKey is a key without its list selectors, as reeve.rules.level for
// reeve.rules[0].level: the settings it is part of.
func PlainKey(key string) string {
	segs, err := parseKey(key)
	if err != nil {
		return key
	}
	names := make([]string, len(segs))
	for i, s := range segs {
		names[i] = s.name
	}
	return strings.Join(names, ".")
}

func (s keySelector) String() string {
	switch {
	case s.append:
		return "[+]"
	case s.match:
		return "[" + s.field + "=" + s.value + "]"
	}
	return "[" + strconv.Itoa(s.index) + "]"
}

// entry finds the one entry of a list a selector names, and its position.
func (s keySelector) entry(list *yaml.Node, where string) (int, error) {
	if !s.match {
		if s.index >= len(list.Content) {
			return 0, fmt.Errorf("%s has no entry %d (it has %d)", where, s.index, len(list.Content))
		}
		return s.index, nil
	}
	found, at := 0, -1
	for i, item := range list.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if v := mapValue(item, s.field); v != nil && v.Kind == yaml.ScalarNode && strings.EqualFold(v.Value, s.value) {
			found++
			at = i
		}
	}
	switch found {
	case 0:
		return 0, fmt.Errorf("%s has no entry %s=%s", where, s.field, s.value)
	case 1:
		return at, nil
	}
	return 0, fmt.Errorf("%s has %d entries %s=%s: name one by its index", where, found, s.field, s.value)
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// replaceKeeping puts v in place of node, keeping node's comments.
func replaceKeeping(node, v *yaml.Node) {
	v.HeadComment, v.LineComment, v.FootComment = node.HeadComment, node.LineComment, node.FootComment
	*node = *v
}
