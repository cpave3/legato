package service

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/sandbox"
)

// A jira credential's guidance works as written from a shell in the agent's
// sandbox: the shell has the placeholder, not the operator's own token that
// the pane inherited, and the proxy signs the request with the real one.
func TestTheJiraGuidanceCurlWorksFromAShellInTheSandbox(t *testing.T) {
	in := bwrapFixture(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}
	t.Setenv("JIRA_TOKEN_SRC", jiraToken)
	f := newBrokerFixtureWith(t, CredentialBrokerOptions{}, jiraCredential(config.CredentialSettings{Upstream: "https://acme.atlassian.net", Username: "bot@example.com"}))
	var proxyURL string
	channels := NewCredentialChannels(f.broker, CredentialChannelsOptions{
		ProxyURL:     func() string { return proxyURL },
		SSHSocketDir: filepath.Join(t.TempDir(), "ssh"),
	})
	jira := &fakeJiraUpstream{user: "bot@example.com"}
	proxyURL = proxyFor(t, channels, "jira", jira.serve(t)).URL
	env, err := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	in.SessionEnv = sessionEnvNames(env)
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}

	script := `printf 'token=%s email=%s base=%s\n' "${JIRA_API_TOKEN%%_*}" "$JIRA_EMAIL" "${JIRA_BASE_URL##*/}"
curl -su "$JIRA_EMAIL:$JIRA_API_TOKEN" "$JIRA_BASE_URL/rest/api/3/search/jql?jql=project%3DAG&fields=summary" -o /dev/null -w 'status=%{http_code}\n'`
	shells := []string{"bash"}
	if _, err := exec.LookPath("zsh"); err == nil {
		shells = append(shells, "zsh")
	}
	for _, shell := range shells {
		args, err := sandbox.Bwrap{}.Wrap(plan, []string{shell, "-c", script})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(args[0], args[1:]...)
		// As the pane has it: the operator's own token inherited, then
		// Legato's session variables over it (tmux's -e).
		cmd.Env = append(os.Environ(), "HOME="+in.Home, "JIRA_API_TOKEN=operator-real-token")
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v: %s", shell, err, out)
		}
		if want := "token=lgk email=bot@example.com base=jira"; !strings.Contains(string(out), want) {
			t.Errorf("%s: want %q in:\n%s", shell, want, out)
		}
		if !strings.Contains(string(out), "status=200") {
			t.Errorf("%s: the guidance's curl failed:\n%s", shell, out)
		}
	}
	if jira.calls != len(shells) {
		t.Fatalf("Jira saw %d calls, want %d", jira.calls, len(shells))
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@example.com:"+jiraToken)); jira.seen[0].Header.Get("Authorization") != want {
		t.Fatalf("Authorization = %q", jira.seen[0].Header.Get("Authorization"))
	}
}
