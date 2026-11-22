package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PathError is a validation problem and where it is in the definition, as a
// path such as steps[2].with.prompt or on.schedule.cron. Its text is the
// problem's alone.
type PathError struct {
	Path string
	Err  error
}

func (e *PathError) Error() string { return e.Err.Error() }
func (e *PathError) Unwrap() error { return e.Err }

// ErrorPath is the path of the problem err names, or "".
func ErrorPath(err error) string {
	var problem *PathError
	if errors.As(err, &problem) {
		return problem.Path
	}
	return ""
}

// within places err at path, under any path err already has.
func within(path string, err error) *PathError {
	var inner *PathError
	if errors.As(err, &inner) {
		path = joinPath(path, inner.Path)
	}
	return &PathError{Path: path, Err: err}
}

func joinPath(outer, inner string) string {
	switch {
	case outer == "":
		return inner
	case inner == "":
		return outer
	case strings.HasPrefix(inner, "["):
		return outer + inner
	}
	return outer + "." + inner
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Severity says whether a Diagnostic stops the file from loading.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is one problem in a workflow file, anchored to its YAML. Lines
// and columns count from 1; the end is exclusive. Path is empty when the
// problem has no place in the definition, such as a YAML syntax error.
type Diagnostic struct {
	Severity  Severity `json:"severity"`
	Message   string   `json:"message"`
	Path      string   `json:"path,omitempty"`
	Line      int      `json:"line"`
	Column    int      `json:"column"`
	EndLine   int      `json:"end_line"`
	EndColumn int      `json:"end_column"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d: %s", d.Line, d.Column, d.Message)
}

// Check parses and validates a workflow file as Parse does, but reports
// every problem it can find, each with its place in the file: a YAML or
// decoding problem stops it, and validation gives at most one problem per
// step. The definition is returned only when there is no error; warnings,
// such as a with key the step's executor does not read, come with it.
func Check(source string, data []byte) (*Definition, []Diagnostic) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, decodeDiagnostics(err, nil)
	}
	if len(root.Content) == 0 {
		return nil, []Diagnostic{{Severity: SeverityError, Message: "the file is empty", Line: 1, Column: 1, EndLine: 1, EndColumn: 2}}
	}
	var definition Definition
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return nil, decodeDiagnostics(err, &root)
	}
	definition.Source = source
	var diagnostics []Diagnostic
	definition.fromFile = true
	problems := definition.problems()
	definition.fromFile = false
	for _, problem := range problems {
		diagnostics = append(diagnostics, diagnosticAt(&root, problem.Path, problem.Error()))
	}
	failed := len(diagnostics) > 0
	diagnostics = append(diagnostics, unknownWithKeys(&root, &definition)...)
	if failed {
		return nil, diagnostics
	}
	return &definition, diagnostics
}

// unknownWithKeys warns of each with key a step's executor does not read.
// The engine ignores them for now; a later release refuses them (LEG-461).
func unknownWithKeys(root *yaml.Node, definition *Definition) []Diagnostic {
	var diagnostics []Diagnostic
	for i, step := range definition.Steps {
		spec, ok := catalogByType[step.Type]
		if !ok {
			continue
		}
		for _, key := range sortedKeys(step.With) {
			if _, known := spec.Field(key); known {
				continue
			}
			d := diagnosticAt(root, fmt.Sprintf("steps[%d].with.%s", i, key), fmt.Sprintf("step %q: %s takes no with field %q, so it is ignored; a later release will refuse it", step.ID, step.Type, key))
			d.Severity = SeverityWarning
			diagnostics = append(diagnostics, d)
		}
	}
	return diagnostics
}

var yamlLinePattern = regexp.MustCompile(`^(?:yaml: )?line (\d+): (.*)$`)
var unknownFieldPattern = regexp.MustCompile(`^field (\S+) not found in type`)

// decodeDiagnostics turns yaml.v3's errors into diagnostics on the lines
// it reports, at the key on that line when root has one.
func decodeDiagnostics(err error, root *yaml.Node) []Diagnostic {
	messages := []string{err.Error()}
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		messages = typeErr.Errors
	} else if errors.Is(err, io.EOF) {
		messages = []string{"the file is empty"}
	}
	var diagnostics []Diagnostic
	for _, message := range messages {
		d := Diagnostic{Severity: SeverityError, Message: strings.TrimPrefix(message, "yaml: "), Line: 1, Column: 1}
		if match := yamlLinePattern.FindStringSubmatch(message); match != nil {
			d.Line, _ = strconv.Atoi(match[1])
			d.Message = match[2]
			name := ""
			if field := unknownFieldPattern.FindStringSubmatch(match[2]); field != nil {
				name = field[1]
			}
			if key, path := keyOnLine(root, d.Line, name); key != nil {
				d.Path = path
				d.Column = key.Column
				d.EndLine, d.EndColumn = key.Line, key.Column+len(key.Value)
			}
		}
		if d.EndLine == 0 {
			d.EndLine, d.EndColumn = d.Line, d.Column+1
		}
		diagnostics = append(diagnostics, d)
	}
	return diagnostics
}

// keyOnLine is the first mapping key on a line (the one named name, when
// name is given) and its path.
func keyOnLine(root *yaml.Node, line int, name string) (*yaml.Node, string) {
	if root == nil {
		return nil, ""
	}
	var found *yaml.Node
	var foundPath string
	var walk func(node *yaml.Node, path string)
	walk = func(node *yaml.Node, path string) {
		if node == nil || found != nil {
			return
		}
		switch node.Kind {
		case yaml.DocumentNode:
			for _, child := range node.Content {
				walk(child, path)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				if key.Line == line && (name == "" || key.Value == name) {
					found, foundPath = key, joinPath(path, key.Value)
					return
				}
				walk(value, joinPath(path, key.Value))
			}
		case yaml.SequenceNode:
			for i, child := range node.Content {
				walk(child, joinPath(path, fmt.Sprintf("[%d]", i)))
			}
		}
	}
	walk(root, "")
	return found, foundPath
}

// diagnosticAt anchors a problem at path: on a one-line scalar value, the
// value; on anything else, its key. A path that goes further than the file
// is anchored where the file stops.
func diagnosticAt(root *yaml.Node, path, message string) Diagnostic {
	d := Diagnostic{Severity: SeverityError, Message: message, Path: path, Line: 1, Column: 1}
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	var key, value *yaml.Node
	// The path is matched against the file's own keys, longest first, so a
	// key with dots in it (task.entered_column) is one key.
	rest := path
	for rest != "" {
		node = resolveAlias(node)
		var nextKey, next *yaml.Node
		consumed := 0
		if strings.HasPrefix(rest, "[") {
			end := strings.Index(rest, "]")
			index, err := strconv.Atoi(rest[1:max(end, 1)])
			if end > 0 && err == nil && node.Kind == yaml.SequenceNode && index < len(node.Content) {
				next, consumed = node.Content[index], end+1
			}
		} else if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				k := node.Content[i].Value
				fits := rest == k || strings.HasPrefix(rest, k+".") || strings.HasPrefix(rest, k+"[")
				if fits && len(k) > consumed {
					nextKey, next, consumed = node.Content[i], node.Content[i+1], len(k)
				}
			}
		}
		if next == nil {
			break
		}
		key, value, node = nextKey, next, next
		rest = strings.TrimPrefix(rest[consumed:], ".")
	}
	value = resolveAlias(value)
	switch {
	case value != nil && value.Kind == yaml.ScalarNode && value.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0:
		d.Line, d.Column = value.Line, value.Column
		d.EndLine, d.EndColumn = value.Line, value.Column+scalarWidth(value)
	case key != nil:
		d.Line, d.Column = key.Line, key.Column
		d.EndLine, d.EndColumn = key.Line, key.Column+len(key.Value)
	case value != nil:
		d.Line, d.Column = value.Line, value.Column
		d.EndLine, d.EndColumn = value.Line, value.Column+1
	default:
		d.EndLine, d.EndColumn = 1, 2
	}
	return d
}

func resolveAlias(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

// scalarWidth is about how many columns a one-line scalar takes in the
// file: its value, and its quotes.
func scalarWidth(node *yaml.Node) int {
	width := len(node.Value)
	if node.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		width += 2
	}
	if width == 0 {
		width = 1
	}
	return width
}
