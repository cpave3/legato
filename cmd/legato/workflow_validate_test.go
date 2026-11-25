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

// A warning (an unknown with key) is printed, and the file is valid.
func TestWorkflowValidateWarnsAndSucceeds(t *testing.T) {
	path := writeWorkflowFile(t, "schema_version: 1\nname: check\ntitle: Check\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with: {prompt: hi, promt: hi}\n")
	var code int
	var stdout string
	stderr, _ := captureStderrWithCode(t, func() int {
		stdout = captureStdout(t, func() int {
			code = runCLI([]string{"workflow", "validate", path})
			return code
		})
		return code
	})
	if code != 0 || !strings.Contains(stdout, `"name":"check"`) {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
	if !strings.Contains(stderr, path+":8:") || !strings.Contains(stderr, "warning: ") || !strings.Contains(stderr, `"promt"`) {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestWorkflowValidateWarnsOfAFallbackThatCanNeverFire(t *testing.T) {
	path := writeWorkflowFile(t, "schema_version: 1\nname: help\ntitle: Help\non:\n  chat.mention:\n    bot: slack\n    fallback: true\nsteps:\n  - {id: a, title: A, type: chat.message.send, with: {bot: slack, text: hi}}\n")
	configDir := t.TempDir()
	t.Setenv("LEGATO_CONFIG", filepath.Join(configDir, "config.yaml"))
	if err := os.MkdirAll(filepath.Join(configDir, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := "schema_version: 1\nname: log\ntitle: Log\non:\n  chat.message:\n    bot: slack\nsteps:\n  - {id: a, title: A, type: chat.message.send, with: {bot: slack, text: hi}}\n"
	if err := os.WriteFile(filepath.Join(configDir, "workflows", "log.yaml"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	var code int
	stderr := captureStderr(t, func() {
		captureStdout(t, func() int {
			code = runCLI([]string{"workflow", "validate", path})
			return code
		})
	})
	if code != 0 || !strings.Contains(stderr, ":7:15: warning: this fallback can never fire: log's chat.message trigger") {
		t.Fatalf("exit %d, stderr = %q", code, stderr)
	}
}
