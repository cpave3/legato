package main

import (
	"strings"
	"testing"
)

// A call that is not a request at all is never made, and does not bring
// the process down.
func TestClassifyCallRefusesWhatDoesNotParse(t *testing.T) {
	if class, _ := classifyCall("DELETE", "/api/tasks/hello world", nil); class != "never" {
		t.Fatalf("a path with a space: %s", class)
	}
	if class, _ := classifyCall("GET", "/api/board", nil); class != "read" {
		t.Fatalf("GET /api/board: %s", class)
	}
}

// Starting an agent whose containers may use the host network, or changing
// that for a running one, is a risky change for the Reeve, whatever the
// route's own class.
func TestTheHostNetworkForAnAgentsContainersIsARiskyChange(t *testing.T) {
	for _, tc := range []struct {
		method, path, body, want string
	}{
		{"POST", "/api/agents/spawn", `{"title":"t"}`, "change"},
		{"POST", "/api/agents/spawn", `{"title":"t","docker_host_network":true}`, "risky"},
		{"POST", "/api/agents/spawn", `{"title":"t","docker_host_network":false}`, "change"},
		{"POST", "/api/tasks/T-1/agent", `{"docker_host_network":true}`, "risky"},
		{"POST", "/api/tasks/T-1/agent", `{"prompt":"hi"}`, "change"},
		{"POST", "/api/agents/T-1/restart", `{"docker_host_network":false}`, "risky"},
	} {
		class, description := classifyCall(tc.method, tc.path, []byte(tc.body))
		if class != tc.want {
			t.Errorf("%s %s %s = %s, want %s", tc.method, tc.path, tc.body, class, tc.want)
		}
		if tc.want == "risky" && !strings.Contains(description, "host network") {
			t.Errorf("%s %s %s described as %q, want it to name the host network", tc.method, tc.path, tc.body, description)
		}
	}
}
