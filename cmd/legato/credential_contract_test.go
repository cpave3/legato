package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// credentialTestConfig writes a configuration with one credential that the
// rules let agents read without asking, and a running agent to read it.
func credentialTestConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	configYAML := "db:\n  path: " + filepath.Join(root, "legato.db") + "\n" +
		"data_dir: " + filepath.Join(root, "data") + "\n" +
		"secrets:\n  master_key: file\n" +
		"credentials:\n" +
		"  - name: eleven\n    secret: env://CONTRACT_ELEVEN_KEY\n    policy:\n      - { operation: read, approve: auto }\n" +
		"  - name: stored\n    secret: legato://stored-key\n    policy:\n      - { operation: read, approve: auto }\n"
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("CONTRACT_ELEVEN_KEY", "sk-eleven")
	t.Setenv("LEGATO_TASK_ID", "task-1")
	t.Setenv("LEGATO_EXECUTION_ID", "ex-1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Narrate", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", AgentKind: "claude-code", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSecretCommandsKeepAValueAndNeverPrintIt(t *testing.T) {
	credentialTestConfig(t)
	secretInput = strings.NewReader("sk-stored\n")
	t.Cleanup(func() { secretInput = os.Stdin })

	if out := captureStdout(t, func() int { return runCLI([]string{"secret", "add", "stored-key"}) }); strings.Contains(out, "sk-stored") {
		t.Fatalf("add printed the value: %s", out)
	}
	out := captureStdout(t, func() int { return runCLI([]string{"secret", "list", "--json"}) })
	if strings.Contains(out, "sk-stored") || !strings.Contains(out, `"stored-key"`) || !strings.Contains(out, `"master_key":"file"`) {
		t.Fatalf("list = %s", out)
	}
	if got := captureStdout(t, func() int { return runCLI([]string{"credential", "get", "stored"}) }); got != "sk-stored" {
		t.Fatalf("get = %q, want the stored value", got)
	}
	if code := runCLI([]string{"secret", "remove", "stored-key"}); code != 0 {
		t.Fatalf("remove exited %d", code)
	}
	if code := runCLI([]string{"secret", "remove", "stored-key"}); code != exitNotFound {
		t.Fatalf("second remove exited %d, want %d", code, exitNotFound)
	}
}

func TestCredentialGetAndExecGiveTheValueOnlyToWhatAsks(t *testing.T) {
	root := credentialTestConfig(t)

	if got := captureStdout(t, func() int { return runCLI([]string{"credential", "get", "eleven"}) }); got != "sk-eleven" {
		t.Fatalf("get = %q", got)
	}
	marker := filepath.Join(root, "seen")
	if code := runCLI([]string{"credential", "exec", "eleven", "--env", "XI_KEY", "--", "sh", "-c", `printf %s "$XI_KEY" > "$0"`, marker}); code != 0 {
		t.Fatalf("exec exited %d", code)
	}
	if data, _ := os.ReadFile(marker); string(data) != "sk-eleven" {
		t.Fatalf("the command saw %q", data)
	}
	_, code := captureStderrWithCode(t, func() int { return runCLI([]string{"credential", "get", "eleven", "--operation", "write"}) })
	if code != exitForbidden {
		t.Fatalf("a use no rule allows exited %d, want %d", code, exitForbidden)
	}
	t.Setenv("LEGATO_EXECUTION_ID", "")
	if _, code := captureStderrWithCode(t, func() int { return runCLI([]string{"credential", "get", "eleven"}) }); code != exitEnvironment {
		t.Fatalf("outside an agent session exited %d, want %d", code, exitEnvironment)
	}
}

func TestCredentialListAndTheAuditLog(t *testing.T) {
	credentialTestConfig(t)
	out := captureStdout(t, func() int { return runCLI([]string{"credential", "list", "--json"}) })
	if !strings.Contains(out, `"name":"eleven"`) || strings.Contains(out, "sk-eleven") {
		t.Fatalf("list = %s", out)
	}
	if code := runCLI([]string{"credential", "request", "eleven", "--operation", "read", "--reason", "narration", "--json"}); code != 0 {
		t.Fatalf("request exited %d", code)
	}
	captureStdout(t, func() int { return runCLI([]string{"credential", "get", "eleven"}) })

	out = captureStdout(t, func() int { return runCLI([]string{"audit", "--credential", "eleven", "--json"}) })
	var envelope struct {
		Data struct {
			Entries []struct {
				Event string `json:"event"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var events []string
	for _, e := range envelope.Data.Entries {
		events = append(events, e.Event)
	}
	if strings.Join(events, ",") != "use,grant,request" || strings.Contains(out, "sk-eleven") {
		t.Fatalf("audit = %v (%s)", events, out)
	}
	if code := runCLI([]string{"audit", "verify"}); code != 0 {
		t.Fatalf("verify exited %d", code)
	}
}

func TestCredentialListAsksTheVaultForItsKeys(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	_ = os.MkdirAll(bin, 0o755)
	fakeOp := "#!/bin/sh\n" +
		"if [ \"$1 $2\" = \"item list\" ]; then echo '[{\"id\":\"id-1\",\"title\":\"legato_default\",\"version\":1,\"vault\":{\"id\":\"v1\",\"name\":\"Legato\"}}]'; exit 0; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "op"), []byte(fakeOp), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	configYAML := "db:\n  path: " + filepath.Join(root, "legato.db") + "\n" +
		"data_dir: " + filepath.Join(root, "data") + "\n" +
		"credential_sources:\n  - vault: op://Legato\n" +
		"secrets:\n  onepassword:\n    backend: op\n"
	configPath := filepath.Join(root, "config.yaml")
	_ = os.WriteFile(configPath, []byte(configYAML), 0o600)
	t.Setenv("LEGATO_CONFIG", configPath)

	out := captureStdout(t, func() int { return runCLI([]string{"credential", "list"}) })
	if !strings.Contains(out, "legato_default") || !strings.Contains(out, "op://Legato") {
		t.Fatalf("list = %q", out)
	}
	if strings.Contains(out, "No credentials") {
		t.Fatalf("list = %q", out)
	}
}
