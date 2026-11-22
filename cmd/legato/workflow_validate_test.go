package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkflowFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "check.workflow.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", filepath.Join(t.TempDir(), "missing-config.yaml"))
	return path
}

const twoBadSteps = `schema_version: 1
name: check
title: Check
steps:
  - id: a
    title: A
    type: agent.nope
  - id: b
    title: B
    type: agent.run
    with: {}
`

func TestWorkflowValidatePrintsEachProblemAtItsLine(t *testing.T) {
	path := writeWorkflowFile(t, twoBadSteps)
	stderr, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"workflow", "validate", path})
	})
	if code != exitUsage {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	want := path + ":7:11: step \"a\" has unsupported type \"agent.nope\"\n" +
		path + ":11:5: step \"b\": with requires prompt or macro\n"
	if stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestWorkflowValidateJSONGivesTheDiagnostics(t *testing.T) {
	path := writeWorkflowFile(t, twoBadSteps)
	stderr, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"workflow", "validate", path, "--json"})
	})
	if code != exitUsage {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	var out struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Diagnostics []struct {
					Path string `json:"path"`
					Line int    `json:"line"`
				} `json:"diagnostics"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &out); err != nil {
		t.Fatalf("stderr %q: %v", stderr, err)
	}
	if out.Error.Code != "workflow_invalid" || len(out.Error.Details.Diagnostics) != 2 || out.Error.Details.Diagnostics[1].Path != "steps[1].with" {
		t.Fatalf("error = %+v", out.Error)
	}
}

func TestWorkflowValidatePrintsAValidDefinition(t *testing.T) {
	path := writeWorkflowFile(t, strings.Replace(strings.Replace(twoBadSteps, "agent.nope\n", "agent.run\n    with: {prompt: hi}\n", 1), "with: {}", "with: {prompt: hi}", 1))
	var code int
	stdout := captureStdout(t, func() int {
		code = runCLI([]string{"workflow", "validate", path})
		return code
	})
	if code != 0 || !strings.Contains(stdout, `"name":"check"`) {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
}
