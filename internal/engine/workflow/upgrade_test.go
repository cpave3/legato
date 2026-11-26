package workflow

import (
	"strings"
	"testing"
)

func upgradeText(t *testing.T, text string) *UpgradeResult {
	t.Helper()
	result, err := Upgrade("case.yaml", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// keep_agent and an agent_ended agent.stop become an agent step that
// waits for complete and keeps its agent, then an agent.stop that waits for
// that agent to end (spec LEG-482 §8). Every byte the upgrade does not
// rewrite stays, comments included.
func TestUpgradeRewritesKeepAgentAndAgentEnded(t *testing.T) {
	in := `# Review, then talk.
schema_version: 1 # the version
name: r
title: R
steps:
  # The agent stays for questions.
  - id: review
    title: Review
    type: agent.run
    with:
      prompt: hi
      keep_agent: true # keep it
  - id: end
    title: End
    type: agent.stop
    trigger: agent_ended
    with:
      task: ${{ workflow.task }}
`
	want := `# Review, then talk.
schema_version: 2 # the version
name: r
title: R
steps:
  # The agent stays for questions.
  - id: review
    title: Review
    type: agent.run
    wait_for: [complete]
    with:
      prompt: hi
      keep: true # keep it
  - id: end
    title: End
    type: agent.stop
    wait_for: [ended]
    with:
      agent: {from: review}
`
	result := upgradeText(t, in)
	if string(result.YAML) != want {
		t.Fatalf("upgraded:\n%s\nwant:\n%s", result.YAML, want)
	}
	if !result.Changed || len(result.Notes) != 0 {
		t.Fatalf("changed %v notes %v", result.Changed, result.Notes)
	}
	definition, err := Parse("r.yaml", result.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.Steps[1]; got.Start != StartAutomatic || got.WaitFor[0] != SignalEnded {
		t.Fatalf("end step = %+v", got)
	}
}

func TestUpgradeRenamesTriggerToStart(t *testing.T) {
	in := "schema_version: 1\nname: s\ntitle: S\nsteps:\n  - id: a\n    title: A\n    type: task.context\n    trigger: manual\n    with: {task: \"${{ workflow.task }}\"}\n  - {id: b, title: B, type: task.context, trigger: automatic, with: {task: \"${{ workflow.task }}\"}}\n  - {id: c, title: C, type: task.context, trigger: '', with: {task: \"${{ workflow.task }}\"}}\n"
	want := "schema_version: 2\nname: s\ntitle: S\nsteps:\n  - id: a\n    title: A\n    type: task.context\n    start: manual\n    with: {task: \"${{ workflow.task }}\"}\n  - {id: b, title: B, type: task.context, start: automatic, with: {task: \"${{ workflow.task }}\"}}\n  - {id: c, title: C, type: task.context, with: {task: \"${{ workflow.task }}\"}}\n"
	result := upgradeText(t, in)
	if string(result.YAML) != want {
		t.Fatalf("upgraded:\n%s\nwant:\n%s", result.YAML, want)
	}
	if _, err := Parse("s.yaml", result.YAML); err != nil {
		t.Fatal(err)
	}
}

// keep_agent without an agent_ended step after it only takes its v2 name.
func TestUpgradeRenamesKeepAgentAlone(t *testing.T) {
	in := "schema_version: 1\nname: s\ntitle: S\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with:\n      prompt: hi\n      keep_agent: true\n    outputs:\n      pr_number: {type: integer}\n"
	want := strings.Replace(strings.Replace(in, "keep_agent", "keep", 1), "schema_version: 1", "schema_version: 2", 1)
	if got := upgradeText(t, in); string(got.YAML) != want {
		t.Fatalf("upgraded:\n%s\nwant:\n%s", got.YAML, want)
	}
}

// A step other than agent.stop that started when the agent ended gets an
// agent.stop before it that waits for the agent to end.
func TestUpgradeWaitsBeforeAnotherAgentEndedStep(t *testing.T) {
	in := `schema_version: 1
name: s
title: S
steps:
  - id: talk
    title: Talk
    type: agent.run
    with:
      prompt: hi
      keep_agent: true
  - id: note
    title: Note
    type: task.note
    trigger: agent_ended
    with:
      task: ${{ workflow.task }}
      body: done
`
	want := `schema_version: 2
name: s
title: S
steps:
  - id: talk
    title: Talk
    type: agent.run
    wait_for: [complete]
    with:
      prompt: hi
      keep: true
  - id: note-agent-ended
    title: Wait for the agent to end
    type: agent.stop
    wait_for: [ended]
    with:
      agent: {from: talk}
  - id: note
    title: Note
    type: task.note
    with:
      task: ${{ workflow.task }}
      body: done
`
	result := upgradeText(t, in)
	if string(result.YAML) != want {
		t.Fatalf("upgraded:\n%s\nwant:\n%s", result.YAML, want)
	}
	if _, err := Parse("s.yaml", result.YAML); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeLeavesAVersion2FileAlone(t *testing.T) {
	in := "schema_version: 2\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: task.context, start: manual, with: {task: \"${{ workflow.task }}\"}}\n"
	result := upgradeText(t, in)
	if result.Changed || string(result.YAML) != in {
		t.Fatalf("changed a v2 file: %s", result.YAML)
	}
}

func TestUpgradeRefusesAFileThatDoesNotLoad(t *testing.T) {
	if _, err := Upgrade("bad.yaml", []byte("schema_version: 1\nname: s\nsteps: []\n")); err == nil {
		t.Fatal("upgraded a file that does not load")
	}
}

// A step rewrite (LEG-599 adds the inline review step's) replaces its step
// and says where each of its outputs went: a later reference to one it
// maps is rewritten, and one it cannot map is flagged, as an error, at that
// reference's line and column.
func TestAStepRewriteFlagsAReferenceItCannotMap(t *testing.T) {
	in := `schema_version: 1
name: f
title: F
inputs:
  task: {type: task}
steps:
  - id: ctx
    title: Read
    type: task.context
    with:
      task: ${{ workflow.inputs.task }}
    outputs:
      extra: {type: string}
  - id: note
    title: Note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body: |
        Context: ${{ steps.ctx.outputs.content }}
        Extra: ${{ steps.ctx.outputs.extra }}
`
	rewrites := map[string]StepRewrite{
		"task.context": func(step *UpgradeStep) {
			if step.ID != "ctx" || step.With["task"] != "${{ workflow.inputs.task }}" {
				t.Errorf("rewrite saw %+v", step)
			}
			step.Replace("- id: read\n  title: Read the card\n  type: task.context\n  with:\n    task: ${{ workflow.inputs.task }}\n")
			step.MapOutput("content", "steps.read.outputs.content")
			step.Unmapped("extra", "the card read declares no extra")
		},
	}
	result, err := upgrade("f.yaml", []byte(in), rewrites)
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.YAML)
	for _, want := range []string{
		"  - id: read\n    title: Read the card\n    type: task.context\n    with:\n      task: ${{ workflow.inputs.task }}\n  - id: note\n",
		"Context: ${{ steps.read.outputs.content }}",
		"Extra: ${{ steps.ctx.outputs.extra }}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("upgraded file has no %q:\n%s", want, text)
		}
	}
	var flagged *Diagnostic
	for i, note := range result.Notes {
		if strings.Contains(note.Message, "the card read declares no extra") {
			flagged = &result.Notes[i]
		}
	}
	if flagged == nil {
		t.Fatalf("no note for extra: %+v", result.Notes)
	}
	// "        Extra: ${{ steps.ctx.outputs.extra }}" is line 19 of the
	// upgraded file; the reference starts at column 20.
	if flagged.Line != 19 || flagged.Column != 20 || flagged.EndColumn != 20+len("steps.ctx.outputs.extra") || flagged.Severity != SeverityError {
		t.Fatalf("note = %+v\n%s", *flagged, text)
	}
	if !strings.Contains(flagged.Message, "steps.ctx.outputs.extra") {
		t.Fatalf("note does not name the reference: %s", flagged.Message)
	}
}
