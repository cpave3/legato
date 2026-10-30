package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gh-token asks Legato for pr.write for a command that writes to a pull
// request, and marks that token to be revoked; anything else is a read.
func TestGHTokenAsksForWhatTheGHCommandNeeds(t *testing.T) {
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Repo, Operation string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		asked = append(asked, in.Operation+" "+in.Repo)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_" + strings.ReplaceAll(in.Operation, ".", "_")})
	}))
	defer proxy.Close()
	t.Setenv("LEGATO_GITHUB_API_URL", proxy.URL+"/c/github/api")
	t.Setenv("LEGATO_GITHUB_TOKEN", "lgk_x")

	edit := captureStdout(t, func() int {
		return runCredentialGHToken([]string{"--", "pr", "edit", "1529", "-R", "autopilot-dev/autopilot"})
	})
	view := captureStdout(t, func() int {
		return runCredentialGHToken([]string{"--", "pr", "view", "1529", "-R", "autopilot-dev/autopilot"})
	})
	if strings.TrimSpace(edit) != "ghs_pr_write revoke" || strings.TrimSpace(view) != "ghs_api_read" {
		t.Fatalf("edit %q view %q", edit, view)
	}
	if len(asked) != 2 || asked[0] != "pr.write autopilot-dev/autopilot" || asked[1] != "api.read autopilot-dev/autopilot" {
		t.Fatalf("asked = %v", asked)
	}
}

// gh pr ready is done by Legato: gh-token asks for it and prints "done" with
// gh's own message, so the wrapper does not run gh.
func TestGHTokenHasLegatoMarkAPullRequestReady(t *testing.T) {
	var got map[string]any
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/c/github/pr-ready" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]string{"number": "1529"})
	}))
	defer proxy.Close()
	t.Setenv("LEGATO_GITHUB_API_URL", proxy.URL+"/c/github/api")
	t.Setenv("LEGATO_GITHUB_TOKEN", "lgk_x")

	out := captureStdout(t, func() int {
		return runCredentialGHToken([]string{"--", "pr", "ready", "1529", "-R", "autopilot-dev/autopilot"})
	})
	if strings.TrimSpace(out) != `done ✓ Pull request autopilot-dev/autopilot#1529 is marked as "ready for review"` {
		t.Fatalf("out = %q", out)
	}
	if got["repo"] != "autopilot-dev/autopilot" || got["number"] != float64(1529) || got["undo"] != false {
		t.Fatalf("asked = %v", got)
	}
	out = captureStdout(t, func() int {
		return runCredentialGHToken([]string{"--", "pr", "ready", "1529", "--undo", "-R", "autopilot-dev/autopilot"})
	})
	if strings.TrimSpace(out) != `done ✓ Pull request autopilot-dev/autopilot#1529 is converted to "draft"` {
		t.Fatalf("undo: out = %q", out)
	}
}
