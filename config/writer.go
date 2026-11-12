package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Writing config.yaml: edits go through yaml.v3 nodes, so the file keeps
// its comments, its order and every value not edited (an ${ENV} reference
// stays one). Each write leaves a timestamped backup beside the file.

// EditFile applies edit to the YAML document at path and writes it back
// atomically, after copying the old file to <path>.<UTC time>.bak. It
// returns the backup's path. A file that does not exist starts empty.
func EditFile(path string, edit func(doc *yaml.Node) error) (string, error) {
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	out, err := EditBytes(old, edit)
	if err != nil {
		return "", err
	}
	backup := ""
	if old != nil {
		backup = BackupPath(path, time.Now())
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return "", fmt.Errorf("backing up %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return backup, err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return backup, err
	}
	return backup, os.Rename(tmp, path)
}

// BackupPath is the backup of path made at t: config.yaml.<UTC time>.bak.
func BackupPath(path string, t time.Time) string {
	return fmt.Sprintf("%s.%s.bak", path, t.UTC().Format("20060102T150405.000"))
}

// EditBytes applies edit to a YAML document and returns the result, for a
// check or a diff before anything is written.
func EditBytes(data []byte, edit func(doc *yaml.Node) error) ([]byte, error) {
	doc, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	before, _ := parseDocument(data)
	if err := edit(doc); err != nil {
		return nil, err
	}
	// Only the text that changed, when the change allows (writer_patch.go).
	if patched, ok := patchDocument(data, before, doc); ok {
		return patched, nil
	}
	return encodeDocument(doc)
}

func parseDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config.yaml is not a mapping of settings")
	}
	return &doc, nil
}

func encodeDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RemoveKey removes a key from the document (see writer_keys.go: a list
// entry too), and any section or list left empty by it. It reports whether
// the key was there.
func RemoveKey(doc *yaml.Node, key string) bool {
	segs, err := parseKey(key)
	if err != nil {
		return false
	}
	return removeKey(doc.Content[0], segs)
}

func removeKey(m *yaml.Node, segs []keySegment) bool {
	if m.Kind != yaml.MappingNode {
		return false
	}
	seg := segs[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != seg.name {
			continue
		}
		child := m.Content[i+1]
		if len(segs) == 1 && len(seg.selectors) == 0 {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
		if !removeIn(child, seg.selectors, segs[1:]) {
			return false
		}
		if len(child.Content) == 0 {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
		}
		return true
	}
	return false
}

// removeIn removes what selectors then the rest of a key name inside node.
func removeIn(node *yaml.Node, selectors []keySelector, rest []keySegment) bool {
	if len(selectors) == 0 {
		if len(rest) == 0 {
			return false
		}
		return removeKey(node, rest)
	}
	if node.Kind != yaml.SequenceNode || selectors[0].append {
		return false
	}
	at, err := selectors[0].entry(node, "")
	if err != nil {
		return false
	}
	if len(selectors) == 1 && len(rest) == 0 {
		node.Content = append(node.Content[:at], node.Content[at+1:]...)
		return true
	}
	item := node.Content[at]
	if !removeIn(item, selectors[1:], rest) {
		return false
	}
	if len(item.Content) == 0 {
		node.Content = append(node.Content[:at], node.Content[at+1:]...)
	}
	return true
}

// SetKey gives a key a value (see writer_keys.go: a list entry, or a key
// inside one, too), making sections as needed and keeping the key's place
// (and comments) when it is there. A list entry it names must exist, unless
// it is [+], which adds one.
func SetKey(doc *yaml.Node, key string, value any) error {
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return err
	}
	segs, err := parseKey(key)
	if err != nil {
		return err
	}
	m := doc.Content[0]
	where := ""
	for depth, seg := range segs {
		last := depth == len(segs)-1
		if where != "" {
			where += "."
		}
		where += seg.name
		if m.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: %s is not a section", key, strings.TrimSuffix(where, "."+seg.name))
		}
		found := mapValue(m, seg.name)
		if len(seg.selectors) == 0 {
			if last {
				if found != nil {
					replaceKeeping(found, &v)
				} else {
					m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.name}, &v)
				}
				return nil
			}
			if found == nil {
				found = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.name}, found)
			}
			m = found
			continue
		}
		if found == nil {
			if !seg.selectors[0].append {
				return fmt.Errorf("%s: there is no list %s", key, where)
			}
			found = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.name}, found)
		}
		for i, sel := range seg.selectors {
			lastSel := last && i == len(seg.selectors)-1
			if found.Kind != yaml.SequenceNode {
				return fmt.Errorf("%s: %s is not a list", key, where)
			}
			if sel.append {
				if lastSel {
					found.Content = append(found.Content, &v)
					return nil
				}
				item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				found.Content = append(found.Content, item)
				found, where = item, where+sel.String()
				continue
			}
			at, err := sel.entry(found, where)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			if lastSel {
				replaceKeeping(found.Content[at], &v)
				return nil
			}
			found, where = found.Content[at], where+sel.String()
		}
		m = found
	}
	return nil
}

// RemoveListEntry removes the entry called name from a list of named
// entries (a sequence whose items have a nameField, or a map keyed by
// name), and the list when it is left empty. It reports whether it was
// there.
func RemoveListEntry(doc *yaml.Node, list, nameField, name string) bool {
	parent, idx := findKey(doc.Content[0], strings.Split(list, "."))
	if parent == nil {
		return false
	}
	node := parent.Content[idx+1]
	removed := false
	switch node.Kind {
	case yaml.SequenceNode:
		for i, item := range node.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(item.Content); j += 2 {
				if item.Content[j].Value == nameField && strings.EqualFold(item.Content[j+1].Value, name) {
					node.Content = append(node.Content[:i], node.Content[i+1:]...)
					removed = true
					break
				}
			}
			if removed {
				break
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == name {
				node.Content = append(node.Content[:i], node.Content[i+2:]...)
				removed = true
				break
			}
		}
	}
	if removed && len(node.Content) == 0 {
		RemoveKey(doc, list)
	}
	return removed
}

// findKey finds the mapping holding a dotted key, and the key's index in it.
func findKey(m *yaml.Node, parts []string) (*yaml.Node, int) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != parts[0] {
			continue
		}
		if len(parts) == 1 {
			return m, i
		}
		if m.Content[i+1].Kind == yaml.MappingNode {
			return findKey(m.Content[i+1], parts[1:])
		}
	}
	return nil, -1
}
