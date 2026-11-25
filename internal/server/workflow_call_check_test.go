package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func callsFile(name, calls string) string {
	return "schema_version: 2\nname: " + name + "\ntitle: " + name + "\nsteps:\n  - id: call\n    type: workflow.run\n    with: {workflow: " + calls + "}\n"
}

// Recursion is a diagnostic across the workflows found (LEG-489): in the
// editor's check, and on the workflows list.
func TestTheCheckAndTheListFindACallThatLeadsBack(t *testing.T) {
	f := newFilesFixture(t)
	f.write(t, f.global, "b.yaml", callsFile("b", "a"))
	code, out := f.call(t, http.MethodPost, "/api/workflow-files/check", map[string]string{"yaml": callsFile("a", "b")})
	if code != http.StatusOK {
		t.Fatalf("check = %d %v", code, out)
	}
	diagnostics, _ := json.Marshal(out["diagnostics"])
	if !strings.Contains(string(diagnostics), "a → b → a") || !strings.Contains(string(diagnostics), `"severity":"error"`) {
		t.Fatalf("diagnostics = %s", diagnostics)
	}

	f.write(t, f.global, "a.yaml", callsFile("a", "b"))
	code, out = f.call(t, http.MethodGet, "/api/workflows", nil)
	if code != http.StatusOK {
		t.Fatalf("list = %d %v", code, out)
	}
	found := 0
	for _, item := range out["definitions"].([]any) {
		definition := item.(map[string]any)
		warnings, _ := json.Marshal(definition["warnings"])
		if strings.Contains(string(warnings), "leads back") {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("%d workflows warn of the recursion, want 2: %v", found, out["definitions"])
	}
}
