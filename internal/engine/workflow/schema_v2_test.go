package workflow_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// resolveSchemaFor is the schema of a version, or with lenient its copy
// that allows unknown with keys.
func resolveSchemaFor(t *testing.T, version int, lenient bool) *jsonschema.Resolved {
	t.Helper()
	data := workflow.JSONSchemaFor(version)
	if lenient {
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		for name, definition := range raw["definitions"].(map[string]any) {
			if strings.HasPrefix(name, "with.") {
				delete(definition.(map[string]any), "additionalProperties")
			}
		}
		data, _ = json.Marshal(raw)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestVersion2HasASchemaOfItsOwn(t *testing.T) {
	var v1, v2 struct {
		ID        string `json:"$id"`
		Extension struct {
			SchemaVersion int `json:"schema_version"`
		} `json:"x-legato"`
	}
	if err := json.Unmarshal(workflow.JSONSchemaFor(workflow.SchemaVersion), &v1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(workflow.JSONSchemaFor(workflow.SchemaVersion2), &v2); err != nil {
		t.Fatal(err)
	}
	if v1.ID != workflow.SchemaID || v2.ID != workflow.SchemaID2 || v1.ID == v2.ID {
		t.Fatalf("$id v1 %q v2 %q", v1.ID, v2.ID)
	}
	if v1.Extension.SchemaVersion != 1 || v2.Extension.SchemaVersion != 2 {
		t.Fatalf("x-legato schema_version v1 %d v2 %d", v1.Extension.SchemaVersion, v2.Extension.SchemaVersion)
	}
	if !strings.HasSuffix(workflow.SchemaID2, "/legato/schemas/workflow-v2.schema.json") {
		t.Fatalf("SchemaID2 = %s", workflow.SchemaID2)
	}
	if string(workflow.JSONSchema()) != string(workflow.JSONSchemaFor(workflow.SchemaVersion)) {
		t.Fatal("JSONSchema is not version 1's")
	}
	if workflow.LatestSchemaVersion != workflow.SchemaVersion2 {
		t.Fatalf("Legato writes version %d", workflow.LatestSchemaVersion)
	}
}

// The v2 schema takes its step types and their with from the catalog, so a
// step type added there needs no schema edit.
func TestTheV2SchemaTakesStepTypesFromTheCatalog(t *testing.T) {
	var schema struct {
		Definitions map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(workflow.JSONSchemaFor(workflow.SchemaVersion2), &schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"step", "loop_query"} {
		enum, _ := schema.Definitions[name].Properties["type"]["enum"].([]any)
		if len(enum) != len(workflow.Catalog()) {
			t.Fatalf("%s type enum has %d types, the catalog %d", name, len(enum), len(workflow.Catalog()))
		}
	}
	for _, spec := range workflow.Catalog() {
		if _, ok := schema.Definitions["with."+spec.Type]; !ok {
			t.Errorf("no with.%s", spec.Type)
		}
	}
	step := schema.Definitions["step"].Properties
	for _, key := range []string{"start", "if", "on_failure", "wait_for", "timeout", "loop", "steps"} {
		if _, ok := step[key]; !ok {
			t.Errorf("a v2 step has no %s", key)
		}
	}
	if _, ok := step["trigger"]; ok {
		t.Error("a v2 step has trigger, version 1's key")
	}
}

func TestTheBundledExamplesAreVersion2(t *testing.T) {
	v1, v2 := resolveSchemaFor(t, workflow.SchemaVersion, false), resolveSchemaFor(t, workflow.SchemaVersion2, false)
	for _, example := range workflow.Examples() {
		definition, err := workflow.Parse(example.Name+".workflow.yaml", example.YAML)
		if err != nil {
			t.Fatal(err)
		}
		// review-pr-passes is left for LEG-599 to withdraw: no release ships
		// a v2 copy built on a deprecated step.
		want, schema := workflow.SchemaVersion2, v2
		if example.Name == "review-pr-passes" {
			want, schema = workflow.SchemaVersion, v1
		}
		if definition.SchemaVersion != want {
			t.Errorf("%s is schema_version %d, want %d", example.Name, definition.SchemaVersion, want)
		}
		if err := schema.Validate(asJSON(t, example.YAML)); err != nil {
			t.Errorf("%s: %v", example.Name, err)
		}
	}
}

func TestTheV2SchemaRefusesInvalidFiles(t *testing.T) {
	schema := resolveSchemaFor(t, workflow.SchemaVersion2, false)
	base := "schema_version: 2\nname: s\ntitle: S\n"
	step := "steps:\n  - id: a\n    title: A\n"
	for name, text := range map[string]string{
		"version 1":             strings.Replace(base, "2", "1", 1) + step + "    type: agent.run\n    with: {prompt: hi}\n",
		"trigger":               base + step + "    type: agent.run\n    trigger: manual\n    with: {prompt: hi}\n",
		"agent_ended start":     base + step + "    type: agent.run\n    start: agent_ended\n    with: {prompt: hi}\n",
		"neither type nor loop": base + step + "    with: {prompt: hi}\n",
		"loop and type":         base + step + "    type: agent.run\n    loop: {repeat: true}\n    steps: [{id: b, title: B, type: agent.run, with: {prompt: hi}}]\n",
		"loop without steps":    base + step + "    loop: {repeat: true}\n",
		"two loop forms":        base + step + "    loop: {repeat: true, over: [1]}\n    steps: [{id: b, title: B, type: agent.run, with: {prompt: hi}}]\n",
		"loop max over 100":     base + step + "    loop: {repeat: true, max: 101}\n    steps: [{id: b, title: B, type: agent.run, with: {prompt: hi}}]\n",
		"bad on_failure":        base + step + "    type: agent.run\n    on_failure: retry\n    with: {prompt: hi}\n",
		"bad wait_for":          base + step + "    type: agent.run\n    wait_for: [soon]\n    with: {prompt: hi}\n",
		"bad timeout":           base + step + "    type: agent.run\n    timeout: a while\n    with: {prompt: hi}\n",
		"bad body step":         base + step + "    loop: {repeat: true}\n    steps: [{id: b, title: B, type: agent.nope}]\n",
		"bad query with":        base + step + "    loop: {next: {type: tickets.list, with: {colour: blue}}, key: x}\n    steps: [{id: b, title: B, type: agent.run, with: {prompt: hi}}]\n",
		"bad output type":       base + "outputs:\n  x: {type: file, value: hi}\n" + step + "    type: agent.run\n    with: {prompt: hi}\n",
	} {
		if err := schema.Validate(asJSON(t, []byte(text))); err == nil {
			t.Errorf("%s: the schema accepts it", name)
		}
	}
}

// Every inline v2 definition this package's tests parse successfully
// matches the v2 schema.
func TestEveryValidV2TestDefinitionMatchesTheV2Schema(t *testing.T) {
	strict, lenient := resolveSchemaFor(t, workflow.SchemaVersion2, false), resolveSchemaFor(t, workflow.SchemaVersion2, true)
	checked := 0
	for _, text := range v2TestDefinitions(t) {
		definition, err := workflow.Parse("case.yaml", []byte(text))
		if err != nil || definition.SchemaVersion != workflow.SchemaVersion2 {
			continue
		}
		checked++
		schema := strict
		if _, diagnostics := workflow.Check("case.yaml", []byte(text)); len(diagnostics) > 0 {
			schema = lenient
		}
		if err := schema.Validate(asJSON(t, []byte(text))); err != nil {
			t.Errorf("a v2 definition Parse accepts fails the v2 schema: %v\n%s", err, text)
		}
	}
	if checked < 10 {
		t.Fatalf("checked only %d v2 test definitions; the source scan is broken", checked)
	}
}

// v2TestDefinitions are the string literals in this package's tests that
// are a whole schema_version 2 file: those passed to Parse and the files
// kept in constants and tables.
func v2TestDefinitions(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			if text, err := strconv.Unquote(literal.Value); err == nil && strings.HasPrefix(text, "schema_version: 2\n") {
				out = append(out, text)
			}
			return true
		})
	}
	return out
}

func TestTheV2SchemaIsPublishedBesideV1(t *testing.T) {
	for _, path := range []string{"../../../web/src/generated/workflow-v2.schema.json", "../../../site/public/schemas/workflow-v2.schema.json"} {
		if !slices.Contains(generatedSchemaFiles(), path) {
			t.Errorf("%s is not generated", path)
		}
		if _, err := os.Stat(filepath.Clean(path)); err != nil {
			t.Error(err)
		}
	}
}
