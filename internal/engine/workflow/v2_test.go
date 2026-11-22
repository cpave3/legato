package workflow_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
	"gopkg.in/yaml.v3"
)

func TestAVersion2FileSaysHowEachStepStarts(t *testing.T) {
	definition, err := workflow.Parse("v2.yaml", []byte(`
schema_version: 2
name: v2
title: V2
steps:
  - id: first
    title: First
    type: agent.run
    with: {prompt: hi}
  - id: second
    title: Second
    type: agent.stop
    start: manual
    with: {task: "${{ workflow.task }}"}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if definition.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d", definition.SchemaVersion)
	}
	if got := definition.Steps[0].Start; got != workflow.StartAutomatic {
		t.Fatalf("first step starts %q, want automatic", got)
	}
	if got := definition.Steps[1].Start; got != workflow.StartManual {
		t.Fatalf("second step starts %q, want manual", got)
	}
	if !definition.Steps[1].WaitsForPerson() {
		t.Fatal("a manual step must wait for the operator")
	}
}

func TestAVersion1TriggerIsReadAsStart(t *testing.T) {
	definition, err := workflow.Parse("v1.yaml", []byte(`
schema_version: 1
name: v1
title: V1
steps:
  - {id: a, title: A, type: agent.run, with: {prompt: hi, keep_agent: true}}
  - {id: b, title: B, type: agent.stop, trigger: agent_ended, with: {task: "${{ workflow.task }}"}}
  - {id: c, title: C, type: task.note, trigger: manual, with: {task: "${{ workflow.task }}", body: done}}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for i, want := range []string{workflow.StartAutomatic, workflow.StartAgentEnded, workflow.StartManual} {
		if got := definition.Steps[i].Start; got != want {
			t.Errorf("step %d starts %q, want %q", i, got, want)
		}
	}
}

func TestEachVersionRefusesTheOthersStartKey(t *testing.T) {
	for name, text := range map[string]string{
		"v1 with start":            "schema_version: 1\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, start: manual, with: {prompt: hi}}\n",
		"v2 with trigger":          "schema_version: 2\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, trigger: manual, with: {prompt: hi}}\n",
		"v2 with agent_ended":      "schema_version: 2\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, start: agent_ended, with: {prompt: hi}}\n",
		"v2 with an unknown start": "schema_version: 2\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, start: later, with: {prompt: hi}}\n",
		"version 3":                "schema_version: 3\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n",
	} {
		if _, err := workflow.Parse("case.yaml", []byte(text)); err == nil {
			t.Errorf("%s: Parse accepted it", name)
		}
	}
}

func TestCatalogOutputsCountAsDeclared(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		_, err := workflow.Parse("catalog.yaml", []byte(`
schema_version: `+version+`
name: catalog
title: Catalog
steps:
  - id: prep
    title: Prepare
    type: repo.ensure
    with: {repo: cpave3/legato}
  - id: work
    title: Work
    type: agent.run
    with:
      prompt: "Work on ${{ steps.prep.outputs.branch }}"
`))
		if err != nil {
			t.Errorf("version %s: a catalog output must be readable without declaring it: %v", version, err)
		}
	}
}

func TestADefinitionKeepsItsStartsThroughJSONAndYAML(t *testing.T) {
	for _, text := range []string{
		"schema_version: 1\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n  - {id: b, title: B, type: agent.stop, trigger: agent_ended, with: {task: \"${{ workflow.task }}\"}}\n",
		"schema_version: 2\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n  - {id: b, title: B, type: agent.stop, start: manual, with: {task: \"${{ workflow.task }}\"}}\n",
	} {
		definition, err := workflow.Parse("case.yaml", []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := yaml.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		again, err := workflow.Parse("again.yaml", encoded)
		if err != nil {
			t.Fatalf("a marshalled definition does not parse again: %v\n%s", err, encoded)
		}
		if again.Steps[1].Start != definition.Steps[1].Start {
			t.Fatalf("YAML round trip starts %q, want %q", again.Steps[1].Start, definition.Steps[1].Start)
		}
		data, _ := json.Marshal(definition)
		var decoded workflow.Definition
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Steps[1].Start != definition.Steps[1].Start {
			t.Fatalf("JSON round trip starts %q, want %q", decoded.Steps[1].Start, definition.Steps[1].Start)
		}
	}
}

// A run started before schema version 2 keeps its definition as JSON with
// trigger; it reads as start.
func TestALegacyStoredDefinitionReadsTriggerAsStart(t *testing.T) {
	legacy := `{"schema_version":1,"name":"s","title":"S","steps":[{"id":"a","title":"A","type":"agent.run","trigger":"automatic","with":{"prompt":"hi"}},{"id":"b","title":"B","type":"agent.stop","trigger":"agent_ended","with":{"task":"x"}}],"source":"s.yaml"}`
	var definition workflow.Definition
	if err := json.Unmarshal([]byte(legacy), &definition); err != nil {
		t.Fatal(err)
	}
	if definition.Steps[1].Start != workflow.StartAgentEnded || definition.Steps[0].Start != workflow.StartAutomatic {
		t.Fatalf("starts = %q, %q", definition.Steps[0].Start, definition.Steps[1].Start)
	}
	data, _ := json.Marshal(definition)
	if strings.Contains(string(data), `"trigger"`) {
		t.Fatalf("a definition is written with start only: %s", data)
	}
}

func TestStepPaths(t *testing.T) {
	if got := workflow.StepPath("", "build"); got != "build" {
		t.Fatalf("top-level path = %q", got)
	}
	if got := workflow.StepPath("each/2", "build"); got != "each/2/build" {
		t.Fatalf("nested path = %q", got)
	}
	parent, id := workflow.SplitStepPath("each/2/build")
	if parent != "each/2" || id != "build" {
		t.Fatalf("split = %q, %q", parent, id)
	}
	parent, id = workflow.SplitStepPath("build")
	if parent != "" || id != "build" {
		t.Fatalf("split top-level = %q, %q", parent, id)
	}
}
