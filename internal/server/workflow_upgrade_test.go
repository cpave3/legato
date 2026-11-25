package server

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// POST /api/workflow-files/upgrade gives a v1 file's text upgraded to v2,
// comments kept, and writes nothing: the editor puts it in its buffer.
func TestUpgradingAWorkflowFileWritesNothing(t *testing.T) {
	f := newFilesFixture(t)
	v1 := reviewFile + "  - id: end\n    title: End\n    type: agent.stop\n    trigger: agent_ended\n    with: {task: \"${{ workflow.task }}\"}\n"
	code, body := f.call(t, http.MethodPost, "/api/workflow-files/upgrade", map[string]string{"yaml": v1})
	if code != http.StatusOK || body["changed"] != true {
		t.Fatalf("%d %v", code, body)
	}
	yaml, _ := body["yaml"].(string)
	for _, want := range []string{"# Review every pull request.\nschema_version: 2\n", "type: agent.run   # the reviewer\n", "wait_for: [ended]", "agent: {from: run}"} {
		if !strings.Contains(yaml, want) {
			t.Errorf("upgraded file has no %q:\n%s", want, yaml)
		}
	}
	if notes, ok := body["notes"].([]any); !ok || len(notes) != 0 {
		t.Fatalf("notes = %v", body["notes"])
	}
	if entries, _ := os.ReadDir(f.global); len(entries) != 0 {
		t.Fatalf("upgrade wrote %v", entries)
	}

	code, body = f.call(t, http.MethodPost, "/api/workflow-files/upgrade", map[string]string{"yaml": yaml})
	if code != http.StatusOK || body["changed"] != false || body["yaml"] != yaml {
		t.Fatalf("a v2 file: %d %v", code, body)
	}

	code, body = f.call(t, http.MethodPost, "/api/workflow-files/upgrade", map[string]string{"yaml": strings.Replace(reviewFile, "agent.run", "agent.nope", 1)})
	if code != http.StatusUnprocessableEntity || body["code"] != "invalid" || body["diagnostics"] == nil {
		t.Fatalf("a file that does not load: %d %v", code, body)
	}
}
