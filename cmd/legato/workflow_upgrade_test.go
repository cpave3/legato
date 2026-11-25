package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const reviewV1 = `schema_version: 1
name: review
title: Review
steps:
  # Talk it through.
  - id: review
    title: Review
    type: agent.run
    with: {prompt: hi, keep_agent: true}
  - id: end
    title: End
    type: agent.stop
    trigger: agent_ended
    with:
      task: ${{ workflow.task }}
`

const reviewV2 = `schema_version: 2
name: review
title: Review
steps:
  # Talk it through.
  - id: review
    title: Review
    type: agent.run
    wait_for: [complete]
    with: {prompt: hi, keep: true}
  - id: end
    title: End
    type: agent.stop
    wait_for: [ended]
    with:
      agent: {from: review}
`

func TestWorkflowUpgradeRewritesTheFileInPlace(t *testing.T) {
	path := writeWorkflowFile(t, reviewV1)
	var code int
	stderr := captureStderr(t, func() {
		captureStdout(t, func() int {
			code = runCLI([]string{"workflow", "upgrade", path})
			return code
		})
	})
	if code != 0 {
		t.Fatalf("exit %d, stderr = %q", code, stderr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != reviewV2 {
		t.Fatalf("file:\n%s\nwant:\n%s", got, reviewV2)
	}
}

func TestWorkflowUpgradeStdoutWritesNothing(t *testing.T) {
	path := writeWorkflowFile(t, reviewV1)
	var code int
	stdout := captureStdout(t, func() int {
		code = runCLI([]string{"workflow", "upgrade", path, "--stdout"})
		return code
	})
	if code != 0 || stdout != reviewV2 {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
	if got, _ := os.ReadFile(path); string(got) != reviewV1 {
		t.Fatal("--stdout changed the file")
	}
}

func TestWorkflowUpgradeJSONSaysWhatChanged(t *testing.T) {
	path := writeWorkflowFile(t, reviewV2)
	var code int
	stdout := captureStdout(t, func() int {
		code = runCLI([]string{"workflow", "upgrade", path, "--json"})
		return code
	})
	var out struct {
		Path    string `json:"path"`
		Changed bool   `json:"changed"`
		Notes   []any  `json:"notes"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout %q: %v", stdout, err)
	}
	if code != 0 || out.Path != path || out.Changed || len(out.Notes) != 0 {
		t.Fatalf("exit %d, out = %+v", code, out)
	}
}

func TestWorkflowUpgradeRefusesAFileThatDoesNotLoad(t *testing.T) {
	path := writeWorkflowFile(t, twoBadSteps)
	stderr, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"workflow", "upgrade", path})
	})
	if code != exitUsage || !strings.Contains(stderr, path+":7:11:") {
		t.Fatalf("exit %d, stderr = %q", code, stderr)
	}
	if got, _ := os.ReadFile(path); string(got) != twoBadSteps {
		t.Fatal("a file that does not load was changed")
	}
}
