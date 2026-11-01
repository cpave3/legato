package main

import (
	"context"
	"encoding/json"
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
