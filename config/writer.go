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
	doc, err := parseDocument(old)
	if err != nil {
		return "", err
	}
	if err := edit(doc); err != nil {
		return "", err
	}
	out, err := encodeDocument(doc)
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
	if err := edit(doc); err != nil {
		return nil, err
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

// RemoveKey removes a dotted key from the document, and any section left
// empty by it. It reports whether the key was there.
func RemoveKey(doc *yaml.Node, key string) bool {
	return removeKey(doc.Content[0], strings.Split(key, "."))
}

func removeKey(m *yaml.Node, parts []string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != parts[0] {
			continue
		}
		if len(parts) == 1 {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
		child := m.Content[i+1]
		if child.Kind != yaml.MappingNode || !removeKey(child, parts[1:]) {
			return false
		}
		if len(child.Content) == 0 {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
		}
		return true
	}
	return false
}

// SetKey gives a dotted key a value, making sections as needed and keeping
// the key's place (and comments) when it is there.
func SetKey(doc *yaml.Node, key string, value any) error {
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return err
	}
	m := doc.Content[0]
	parts := strings.Split(key, ".")
	for depth, part := range parts {
		var found *yaml.Node
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == part {
				found = m.Content[i+1]
				if depth == len(parts)-1 {
					v.HeadComment, v.LineComment = found.HeadComment, found.LineComment
					*found = v
					return nil
				}
			}
		}
		if depth == len(parts)-1 {
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: part}, &v)
			return nil
		}
		if found == nil {
			found = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: part}, found)
		}
		if found.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: %s is not a section", key, part)
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
