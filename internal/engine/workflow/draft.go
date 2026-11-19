package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PauseTriggers sets enabled: false on every trigger under a definition's
// on:, whatever the draft said: a drafted workflow never starts by itself
// until the operator switches it on.
func PauseTriggers(data []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("a workflow definition is a YAML mapping")
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "on" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		on := root.Content[i+1]
		for j := 0; j+1 < len(on.Content); j += 2 {
			trigger := on.Content[j+1]
			if trigger.Kind != yaml.MappingNode {
				// An empty trigger (null): make it a mapping that says so.
				*trigger = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			set := false
			for k := 0; k+1 < len(trigger.Content); k += 2 {
				if trigger.Content[k].Value == "enabled" {
					trigger.Content[k+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
					set = true
				}
			}
			if !set {
				trigger.Content = append(trigger.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
			}
		}
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return nil, err
	}
	return out.Bytes(), encoder.Close()
}

// SaveDraft checks a drafted definition as legato workflow validate does,
// pauses every trigger, and writes it to dir as <name>.workflow.yaml. A
// name taken says, or a file there, is refused with ErrWorkflowNameTaken:
// a draft never overwrites a workflow.
func SaveDraft(data []byte, dir string, taken func(name string) bool) (string, error) {
	if _, err := Parse("draft.workflow.yaml", data); err != nil {
		return "", err
	}
	paused, err := PauseTriggers(data)
	if err != nil {
		return "", err
	}
	definition, err := Parse("draft.workflow.yaml", paused)
	if err != nil {
		return "", err
	}
	if !workflowNamePattern.MatchString(definition.Name) {
		return "", fmt.Errorf("%q is not a workflow name: use lower-case letters, digits, '.', '-' and '_'", definition.Name)
	}
	if taken != nil && taken(definition.Name) {
		return "", fmt.Errorf("%w: %s", ErrWorkflowNameTaken, definition.Name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, definition.Name+".workflow.yaml")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("%w: %s", ErrWorkflowNameTaken, path)
	}
	if err != nil {
		return "", err
	}
	if _, err := file.Write(paused); err != nil {
		file.Close()
		return "", err
	}
	return path, file.Close()
}
