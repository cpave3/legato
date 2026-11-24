package workflow_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"

	"github.com/cpave3/legato/internal/engine/workflow"
)

var updateSchema = flag.Bool("update", false, "rewrite the generated workflow JSON Schema for the web UI")

// The web UI's copy of the schema is generated from Go; this fails when it
// is stale.
// The docs site publishes it at its $id, for editors of the operator's own.
var generatedSchemas = []string{
	"../../../web/src/generated/workflow.schema.json",
	"../../../site/public/schemas/workflow-v1.schema.json",
}

func TestTheGeneratedSchemaIsCurrent(t *testing.T) {
	want := workflow.JSONSchema()
	for _, path := range generatedSchemas {
		if *updateSchema {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./internal/engine/workflow -run TestTheGeneratedSchemaIsCurrent -update)", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is stale: run go test ./internal/engine/workflow -run TestTheGeneratedSchemaIsCurrent -update", path)
		}
	}
}

func TestTheSchemaIDIsWhereTheSitePublishesIt(t *testing.T) {
	if !strings.HasSuffix(workflow.SchemaID, "/legato/schemas/"+filepath.Base(generatedSchemas[1])) {
		t.Fatalf("SchemaID %s is not the site's copy %s", workflow.SchemaID, generatedSchemas[1])
	}
}

func TestJSONSchemaIsStable(t *testing.T) {
	if !bytes.Equal(workflow.JSONSchema(), workflow.JSONSchema()) {
		t.Fatal("JSONSchema differs between calls")
	}
}

func resolvedSchema(t *testing.T) *jsonschema.Resolved { return resolveSchema(t, false) }

// resolveSchema is the schema, or with lenient its copy that allows unknown
// with keys, as the editor does when it shows them as warnings (LEG-461).
func resolveSchema(t *testing.T, lenient bool) *jsonschema.Resolved {
	t.Helper()
	data := workflow.JSONSchema()
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

// asJSON is a YAML document as JSON values, as an editor's validator sees
// it.
func asJSON(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEveryExampleAndBuiltInMatchesTheSchema(t *testing.T) {
	schema := resolvedSchema(t)
	examples, err := filepath.Glob("examples/*.yaml")
	if err != nil || len(examples) == 0 {
		t.Fatalf("examples: %v %v", examples, err)
	}
	for _, path := range examples {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(asJSON(t, data)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

// Every inline definition this package's tests parse successfully also
// matches the schema. The cases are read from the test files' source, so a
// new test case is checked too.
func TestEveryValidTestDefinitionMatchesTheSchema(t *testing.T) {
	strict, lenient := resolvedSchema(t), resolveSchema(t, true)
	checked := 0
	for _, text := range parsedTestDefinitions(t) {
		definition, err := workflow.Parse("case.yaml", []byte(text))
		if err != nil || definition.SchemaVersion != workflow.SchemaVersion {
			// The published schema is version 1's (LEG-484 adds 2's).
			continue
		}
		checked++
		// A file with warnings (unknown with keys) matches the schema but
		// for those keys, which the editor shows as warnings too.
		schema := strict
		if _, diagnostics := workflow.Check("case.yaml", []byte(text)); len(diagnostics) > 0 {
			schema = lenient
		}
		if err := schema.Validate(asJSON(t, []byte(text))); err != nil {
			t.Errorf("a definition Parse accepts fails the schema: %v\n%s", err, text)
		}
	}
	if checked < 10 {
		t.Fatalf("checked only %d test definitions; the source scan is broken", checked)
	}
}

func TestTheSchemaRefusesInvalidFiles(t *testing.T) {
	schema := resolvedSchema(t)
	base := "schema_version: 1\nname: s\ntitle: S\n"
	step := "steps:\n  - id: a\n    title: A\n"
	for name, text := range map[string]string{
		"no steps":            base + "steps: []\n",
		"no schema_version":   strings.Replace(base, "schema_version: 1\n", "", 1) + step + "    type: agent.run\n    with: {prompt: hi}\n",
		"agent.run, no with":  base + step + "    type: agent.run\n",
		"wrong version":       strings.Replace(base, "1", "2", 1) + step + "    type: agent.run\n    with: {prompt: hi}\n",
		"unknown top key":     base + "colour: blue\n" + step + "    type: agent.run\n    with: {prompt: hi}\n",
		"unknown step type":   base + step + "    type: agent.nope\n",
		"prompt and macro":    base + step + "    type: agent.run\n    with: {prompt: hi, macro: m}\n",
		"neither":             base + step + "    type: agent.run\n    with: {name: x}\n",
		"missing required":    base + step + "    type: task.move\n    with: {task: \"${{ workflow.task }}\"}\n",
		"unknown with key":    base + step + "    type: agent.run\n    with: {prompt: hi, promt: hi}\n",
		"literal boolean":     base + step + "    type: task.note\n    with: {task: x, body: b, optional: \"${{ workflow.inputs.x }}\"}\n",
		"task not a ref":      base + step + "    type: task.context\n    with: {task: abc}\n",
		"bad scope":           base + step + "    type: github.pr.review_passes\n    with: {repo: a/b, pr_number: 1, scope: all}\n",
		"bad input type":      base + "inputs:\n  x: {type: number}\n" + step + "    type: agent.run\n    with: {prompt: hi}\n",
		"bad step trigger":    base + step + "    type: agent.run\n    trigger: later\n    with: {prompt: hi}\n",
		"bad catch_up":        base + "on:\n  schedule: {cron: \"0 9 * * *\", catch_up: never}\n" + step + "    type: agent.run\n    with: {prompt: hi}\n",
		"unknown trigger key": base + "on:\n  schedule: {cron: \"0 9 * * *\", when: now}\n" + step + "    type: agent.run\n    with: {prompt: hi}\n",
		"bad slot name":       base + step + "    type: agent.run\n    with: {prompt: hi, task: x, slot: Triage}\n",
		"reserved slot":       base + step + "    type: agent.run\n    with: {prompt: hi, task: x, slot: reviewer}\n",
		"slot without task":   base + step + "    type: agent.run\n    with: {prompt: hi, slot: triage}\n",
		"bad condition key":   base + "on:\n  task.entered_column: {column: Doing, conditions: {field: task.id, is: x}}\n" + step + "    type: agent.run\n    with: {prompt: hi}\n",
	} {
		if err := schema.Validate(asJSON(t, []byte(text))); err == nil {
			t.Errorf("%s: the schema accepts it", name)
		}
	}
}

// Files the engine accepts that the schema must too, beside the test files'.
func TestTheSchemaAcceptsWhatTheEngineDoes(t *testing.T) {
	schema := resolvedSchema(t)
	for name, text := range map[string]string{
		"a task from an earlier step": "schema_version: 1\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}, outputs: {task: {type: json}}}\n  - {id: b, title: B, type: task.metadata.set, with: {task: \"${{ steps.a.outputs.task }}\", values: {x: 1}}}\n",
		"an empty step trigger":       "schema_version: 1\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, trigger: '', with: {prompt: hi}}\n",
		"a step with no with":         "schema_version: 1\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: task.note}\n",
		"an agent in a card's slot":   "schema_version: 1\nname: s\ntitle: S\ninputs:\n  task: {type: task}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi, task: \"${{ workflow.inputs.task }}\", repo: a/b, slot: triage}}\n",
	} {
		if _, err := workflow.Parse("s.yaml", []byte(text)); err != nil {
			t.Fatalf("%s: the engine refuses it: %v", name, err)
		}
		if err := schema.Validate(asJSON(t, []byte(text))); err != nil {
			t.Errorf("%s: the schema refuses it: %v", name, err)
		}
	}
}

func TestTheSchemaCarriesTheEditorsExtensions(t *testing.T) {
	var schema struct {
		Definitions map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
			Extension  map[string]any            `json:"x-legato"`
		} `json:"definitions"`
		Extension struct {
			SchemaVersion int `json:"schema_version"`
			Executors     []struct {
				Type string `json:"type"`
			} `json:"executors"`
			Expressions map[string]workflow.ExpressionContext `json:"expressions"`
		} `json:"x-legato"`
	}
	if err := json.Unmarshal(workflow.JSONSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Extension.SchemaVersion != workflow.SchemaVersion || len(schema.Extension.Executors) != len(workflow.Catalog()) {
		t.Fatalf("x-legato = %+v", schema.Extension)
	}
	for _, location := range []string{"run_title", "with", "output_path", "trigger.task.entered_column", "trigger.schedule", "trigger.pr.changes_requested"} {
		if len(schema.Extension.Expressions[location].Roots) == 0 {
			t.Errorf("no expression context for %s", location)
		}
	}
	run := schema.Definitions["with.agent.run"]
	if run.Extension["declared_outputs"] != true {
		t.Errorf("with.agent.run x-legato = %v", run.Extension)
	}
	macro := run.Properties["macro"]["x-legato"].(map[string]any)
	if macro["kind"] != "macro" || macro["expressions"] != true {
		t.Errorf("macro x-legato = %v", macro)
	}
	keep := run.Properties["keep_agent"]["x-legato"].(map[string]any)
	if keep["expressions"] != false {
		t.Errorf("keep_agent x-legato = %v", keep)
	}
	prompt := run.Properties["prompt"]["x-legato"].(map[string]any)
	if prompt["essential"] != true || prompt["label"] != "What it should do" || prompt["order"] != float64(1) {
		t.Errorf("prompt x-legato = %v", prompt)
	}
	if name := run.Properties["name"]["x-legato"].(map[string]any); name["essential"] != nil {
		t.Errorf("name x-legato = %v, want no essential", name)
	}
	if summary, _ := run.Extension["summary"].([]any); len(summary) != 2 || summary[0] != "macro" || summary[1] != "prompt" {
		t.Errorf("with.agent.run summary = %v", run.Extension["summary"])
	}
	if run.Extension["group"] != "Agents" {
		t.Errorf("with.agent.run group = %v", run.Extension["group"])
	}
	for _, kind := range workflow.LearningStepTypes {
		if group := schema.Definitions["with."+kind].Extension["group"]; group != "Learn" {
			t.Errorf("%s group = %v, want Learn: any workflow may use it", kind, group)
		}
	}
}

// parsedTestDefinitions are the string literals this package's tests pass
// to workflow.Parse.
func parsedTestDefinitions(t *testing.T) []string {
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
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Parse" {
				return true
			}
			conversion, ok := call.Args[1].(*ast.CallExpr)
			if !ok || len(conversion.Args) != 1 {
				return true
			}
			if literal, ok := conversion.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				if text, err := strconv.Unquote(literal.Value); err == nil {
					out = append(out, text)
				}
			}
			return true
		})
	}
	return out
}
