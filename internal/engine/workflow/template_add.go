package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrWorkflowNameTaken refuses a name a workflow has, or a file that
// exists: adding a template never overwrites.
var ErrWorkflowNameTaken = errors.New("a workflow of that name exists; choose another name")

var workflowNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

var nameLine = regexp.MustCompile(`(?m)^name: .*$`)

// AddTemplate writes a bundled template into dir as name (the template's
// own name when empty), changing nothing in it but its name line. A name
// taken says (any workflow found has it), or a file of that name in dir,
// is refused with ErrWorkflowNameTaken.
func AddTemplate(template, name, dir string, taken func(name string) bool) (string, error) {
	example, ok := ExampleNamed(template)
	if !ok {
		return "", fmt.Errorf("no template %q", template)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = template
	}
	if err := ValidName(name); err != nil {
		return "", err
	}
	if taken != nil && taken(name) {
		return "", fmt.Errorf("%w: %s", ErrWorkflowNameTaken, name)
	}
	path := filepath.Join(dir, name+".workflow.yaml")
	data := example.YAML
	if name != template {
		data = []byte(nameLine.ReplaceAllStringFunc(string(data), func(line string) string {
			if line == "name: "+template {
				return "name: " + name
			}
			return line
		}))
	}
	if _, err := Parse(path, data); err != nil {
		return "", err
	}
	return CreateFile(dir, name, data)
}
