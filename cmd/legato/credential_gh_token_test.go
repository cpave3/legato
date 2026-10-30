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

	edit := captureStdout(t, func() int { return runCredentialGHToken([]string{"--", "pr", "edit", "1529", "-R", "autopilot-dev/autopilot"}) })
	view := captureStdout(t, func() int { return runCredentialGHToken([]string{"--", "pr", "view", "1529", "-R", "autopilot-dev/autopilot"}) })
	if strings.TrimSpace(edit) != "ghs_pr_write revoke" || strings.TrimSpace(view) != "ghs_api_read" {
		t.Fatalf("edit %q view %q", edit, view)
	}
	if len(asked) != 2 || asked[0] != "pr.write autopilot-dev/autopilot" || asked[1] != "api.read autopilot-dev/autopilot" {
		t.Fatalf("asked = %v", asked)
	}
}
