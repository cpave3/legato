package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

func TestIntegrationTokenCommandsCreateListAndRevoke(t *testing.T) {
	credentialTestConfig(t)
	out := captureStdout(t, func() int {
		return runCLI([]string{"integration", "token", "create", "argus", "--scope", "owner:read,delegate", "--json"})
	})
	var created struct {
		Data struct {
			Name   string   `json:"name"`
			Token  string   `json:"token"`
			Scopes []string `json:"scopes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || !strings.HasPrefix(created.Data.Token, "lgi_") || len(created.Data.Scopes) != 2 {
		t.Fatalf("create = %q (%v)", out, err)
	}

	cfg, _ := config.Load()
	db, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tokens := service.NewIntegrationTokenService(db)
	if id, err := tokens.Authenticate(context.Background(), created.Data.Token); err != nil || id.Name != "argus" {
		t.Fatalf("authenticate = %+v, %v", id, err)
	}
	db.Close()

	out = captureStdout(t, func() int { return runCLI([]string{"integration", "token", "list"}) })
	if !strings.Contains(out, "argus") || !strings.Contains(out, "owner:read,delegate") || strings.Contains(out, created.Data.Token) {
		t.Fatalf("list = %q", out)
	}
	if code := runCLI([]string{"integration", "token", "create", "argus", "--scope", "tasks:write"}); code == 0 {
		t.Fatal("unknown scope accepted")
	}
	if code := runCLI([]string{"integration", "token", "revoke", "argus"}); code != 0 {
		t.Fatalf("revoke exit %d", code)
	}
	out = captureStdout(t, func() int { return runCLI([]string{"integration", "token", "list"}) })
	if !strings.Contains(out, "revoked") {
		t.Fatalf("list after revoke = %q", out)
	}
	if code := runCLI([]string{"integration", "token", "revoke", "argus"}); code != exitNotFound {
		t.Fatalf("second revoke exit %d", code)
	}
}

func TestIntegrationStatusSaysWhenLegatoIsNotConnected(t *testing.T) {
	credentialTestConfig(t)
	out := captureStdout(t, func() int { return runCLI([]string{"integration", "status", "argus"}) })
	if !strings.Contains(out, "Not connected to Argus") || !strings.Contains(out, "0 waiting") {
		t.Fatalf("status = %q", out)
	}
	if code := runCLI([]string{"integration", "status", "jira"}); code == 0 {
		t.Fatal("an unknown integration was accepted")
	}
}

func TestIntegrationCommandsKnowBurnlite(t *testing.T) {
	credentialTestConfig(t)
	out := captureStdout(t, func() int { return runCLI([]string{"integration", "status", "burnlite"}) })
	if !strings.Contains(out, "Not connected to Burnlite") || !strings.Contains(out, "legato integration connect burnlite") || strings.Contains(out, "Replies") {
		t.Fatalf("status = %q", out)
	}
	// With no address given or configured, connect says what to give.
	code, out := captureFailure(t, func() int { return runCLI([]string{"integration", "connect", "burnlite", "--json"}) })
	if code == 0 || !strings.Contains(out, "missing_url") || !strings.Contains(out, "burnlite.url") {
		t.Fatalf("connect exited %d: %q", code, out)
	}
	if code := runCLI([]string{"integration", "disconnect", "burnlite"}); code != 0 {
		t.Fatalf("disconnect exited %d", code)
	}
}

// captureFailure runs a command that is expected to fail and gives its exit
// code and everything it wrote.
func captureFailure(t *testing.T, run func() int) (int, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = w, w
	code := run()
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ := io.ReadAll(r)
	return code, string(out)
}
