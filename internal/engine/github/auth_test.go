package github

import (
	"errors"
	"strings"
	"testing"
)

type recordingTokenCommand struct {
	calls int
	name  string
	args  []string
	out   []byte
	err   error
}

func (r *recordingTokenCommand) Run(name string, args ...string) ([]byte, error) {
	r.calls++
	r.name = name
	r.args = append([]string(nil), args...)
	return r.out, r.err
}

func TestResolveCredentialAutoPrefersConfiguredToken(t *testing.T) {
	runner := &recordingTokenCommand{out: []byte("gh-secret")}
	credential, err := resolveCredential(AuthOptions{Mode: AuthAuto, Token: " configured-secret ", Command: runner})
	if err != nil {
		t.Fatal(err)
	}
	if credential.token != "configured-secret" || credential.source != CredentialConfigured || runner.calls != 0 {
		t.Fatalf("credential=%#v calls=%d", credential, runner.calls)
	}
}

func TestResolveCredentialAutoFallsBackToGitHubCLIOnce(t *testing.T) {
	runner := &recordingTokenCommand{out: []byte(" gh-secret\n")}
	credential, err := resolveCredential(AuthOptions{Mode: AuthAuto, Command: runner})
	if err != nil {
		t.Fatal(err)
	}
	if credential.token != "gh-secret" || credential.source != CredentialGitHubCLI || runner.calls != 1 || runner.name != "gh" || strings.Join(runner.args, " ") != "auth token" {
		t.Fatalf("credential=%#v command=%s %v calls=%d", credential, runner.name, runner.args, runner.calls)
	}
}

func TestResolveCredentialModes(t *testing.T) {
	t.Run("token requires configured token", func(t *testing.T) {
		_, err := resolveCredential(AuthOptions{Mode: AuthToken, Command: &recordingTokenCommand{out: []byte("unused")}})
		if err == nil || !strings.Contains(err.Error(), "github.token") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("gh ignores configured token", func(t *testing.T) {
		runner := &recordingTokenCommand{out: []byte("gh-secret")}
		credential, err := resolveCredential(AuthOptions{Mode: AuthGitHubCLI, Token: "configured-secret", Command: runner})
		if err != nil || credential.token != "gh-secret" || runner.calls != 1 {
			t.Fatalf("credential=%#v calls=%d err=%v", credential, runner.calls, err)
		}
	})
	t.Run("unknown mode", func(t *testing.T) {
		_, err := resolveCredential(AuthOptions{Mode: "write-everything"})
		if err == nil || !strings.Contains(err.Error(), "auto, gh, or token") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestResolveCredentialSanitizesGitHubCLIError(t *testing.T) {
	secret := "token-that-must-not-leak"
	runner := &recordingTokenCommand{out: []byte(secret), err: errors.New(secret)}
	_, err := resolveCredential(AuthOptions{Mode: AuthGitHubCLI, Command: runner})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("unsafe or unactionable error: %v", err)
	}
}

func TestResolveCredentialRejectsEmptyGitHubCLIToken(t *testing.T) {
	_, err := resolveCredential(AuthOptions{Mode: AuthGitHubCLI, Command: &recordingTokenCommand{out: []byte(" \n")}})
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("error = %v", err)
	}
}
