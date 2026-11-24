package workflow

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// LearningStepTypes are the step types of Legato Learn: a task's agent asked
// to reflect, the bounded packet a curator reads, and the deterministic
// ingestion of the curator's operations.
var LearningStepTypes = []string{"agent.interview", "learning.packet.prepare", "learning.proposals.submit"}

// UsesLearning says whether the definition has a Learn step. A run of such a
// definition is a Learn run: its own task is marked so that it never starts
// another (task.metadata.legato.learning_workflow).
func (d *Definition) UsesLearning() bool {
	for _, step := range d.Steps {
		for _, kind := range LearningStepTypes {
			if step.Type == kind {
				return true
			}
		}
	}
	return false
}

// LearnTemplateOptions are what the deprecated learning.* settings said
// about automatic Learn, carried into a learn workflow once. A zero field
// keeps the template's own.
type LearnTemplateOptions struct {
	Agent      string
	Tier       string
	WorkingDir string
	MaxItems   int
	MaxBytes   int
}

// LearnTemplateFor is the bundled learn template named name and triggered
// by column, with the options written into its steps. Its comments are
// kept, and its trigger stays paused: a Legato switches it on for itself.
func LearnTemplateFor(name, column string, options LearnTemplateOptions) ([]byte, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}
	column = strings.TrimSpace(column)
	if column == "" {
		return nil, errors.New("a column is required")
	}
	example, ok := ExampleNamed("learn")
	if !ok {
		return nil, errors.New("the learn template is not bundled")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(example.YAML, &doc); err != nil {
		return nil, err
	}
	root := doc.Content[0]
	setScalar(root, "name", name)
	description := mappingValue(root, "description")
	description.Value = strings.Replace(description.Value, "enters Review", "enters "+column, 1)
	setScalar(mappingValue(mappingValue(root, "on"), TriggerTaskEnteredColumn), "column", column)
	for _, step := range mappingValue(root, "steps").Content {
		with := mappingValue(step, "with")
		switch scalarValue(step, "id") {
		case "packet":
			limits := mappingValue(with, "limits")
			if options.MaxItems > 0 {
				setInt(limits, "max_items", options.MaxItems)
			}
			if options.MaxBytes > 0 {
				setInt(limits, "max_bytes", options.MaxBytes)
			}
		case "curator", "critic":
			if options.Agent != "" {
				setScalar(with, "agent", options.Agent)
			}
			if options.Tier != "" {
				setScalar(with, "tier", options.Tier)
			}
			if options.WorkingDir != "" {
				setScalar(with, "working_dir", options.WorkingDir)
			}
		}
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	if _, err := Parse(name+".workflow.yaml", out.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// mappingValue is the value under key in a mapping node, or an empty
// mapping (not in the document) when there is none.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				return node.Content[i+1]
			}
		}
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func scalarValue(node *yaml.Node, key string) string { return mappingValue(node, key).Value }

func setScalar(node *yaml.Node, key, value string) {
	set(node, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setInt(node *yaml.Node, key string, value int) {
	set(node, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)})
}

func set(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			value.Style = node.Content[i+1].Style &^ (yaml.LiteralStyle | yaml.FoldedStyle)
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
