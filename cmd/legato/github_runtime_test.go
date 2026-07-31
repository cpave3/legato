package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	gh "github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

type runtimeTokenCommand struct {
	calls int
	out   []byte
	err   error
}

func (r *runtimeTokenCommand) Run(name string, args ...string) ([]byte, error) {
	r.calls++
	if name != "gh" || strings.Join(args, " ") != "auth token" {
		panic("unexpected credential command")
	}
	return r.out, r.err
}

func TestBuildWorkflowServiceDoesNotRequireGitHubAuthenticationForLocalWorkflows(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := &config.Config{GitHub: config.GitHubConfig{Auth: "token"}}
	workflowService, err := buildWorkflowService(db, cfg, nil, nil, nil, nil, t.TempDir())
	if err != nil || workflowService == nil {
		t.Fatalf("service=%v err=%v", workflowService, err)
	}
}

func TestTaskGitHubContextDoesNotRequireAuthentication(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\ngithub:\n  auth: token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	if code := runTaskGitHubContract([]string{"context", "task-1", "--repo", "acme/widget", "--branch", "feature/task-1"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	db, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task, _ := db.GetTask(context.Background(), "task-1")
	codeContext, _ := store.ParseCodeContext(task.CodeContext)
	if codeContext == nil || codeContext.Repo != "acme/widget" {
		t.Fatalf("code context = %#v", codeContext)
	}
}

func TestBuildGitHubClientUsesSharedCredentialResolution(t *testing.T) {
	runner := &runtimeTokenCommand{out: []byte("gh-secret\n")}
	client, source, err := buildGitHubClient(&config.Config{GitHub: config.GitHubConfig{Auth: "auto"}}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if client == nil || source != gh.CredentialGitHubCLI || runner.calls != 1 {
		t.Fatalf("client=%v source=%q calls=%d", client, source, runner.calls)
	}

	runner = &runtimeTokenCommand{out: []byte("unused")}
	client, source, err = buildGitHubClient(&config.Config{GitHub: config.GitHubConfig{Auth: "auto", Token: "configured"}}, runner)
	if err != nil || client == nil || source != gh.CredentialConfigured || runner.calls != 0 {
		t.Fatalf("client=%v source=%q calls=%d err=%v", client, source, runner.calls, err)
	}
}
