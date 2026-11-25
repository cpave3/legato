package workflow

import (
	"bytes"
	"errors"
	"fmt"

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

// PausedDraft checks a drafted definition, upgrades a schema_version 1
// draft to version 2 (drafts are written as the version Legato writes),
// pauses its triggers, and checks the result again: every trigger the
// parsed definition has (however the YAML gives it: a merge key, an alias)
// must now be paused, or the draft is refused. It returns the paused YAML
// and its definition.
func PausedDraft(data []byte) ([]byte, *Definition, error) {
	upgraded, err := Upgrade("draft.workflow.yaml", data)
	if err != nil {
		return nil, nil, err
	}
	if len(upgraded.Notes) > 0 {
		note := upgraded.Notes[0]
		return nil, nil, fmt.Errorf("write it as schema_version %d: upgrading it from version 1 left this to do by hand: %s", LatestSchemaVersion, note)
	}
	data = upgraded.YAML
	paused, err := PauseTriggers(data)
	if err != nil {
		return nil, nil, err
	}
	definition, err := Parse("draft.workflow.yaml", paused)
	if err != nil {
		return nil, nil, err
	}
	for _, trigger := range definition.On.List() {
		if trigger.EnabledByDefault {
			return nil, nil, fmt.Errorf("the %s trigger could not be paused: write on: as a plain mapping, without YAML merge keys or aliases", trigger.Kind)
		}
	}
	return paused, definition, nil
}

// SaveDraft checks a drafted definition as legato workflow validate does,
// pauses every trigger, and writes it to dir as <name>.workflow.yaml. A
// name taken says, or a file there, is refused with ErrWorkflowNameTaken:
// a draft never overwrites a workflow.
func SaveDraft(data []byte, dir string, taken func(name string) bool) (string, error) {
	paused, definition, err := PausedDraft(data)
	if err != nil {
		return "", err
	}
	if err := ValidName(definition.Name); err != nil {
		return "", err
	}
	if taken != nil && taken(definition.Name) {
		return "", fmt.Errorf("%w: %s", ErrWorkflowNameTaken, definition.Name)
	}
	return CreateFile(dir, definition.Name, paused)
}
