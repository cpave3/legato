package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestNewRequiresToken(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestListRepositoryPRsConvertsGitHubResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/pulls" || r.URL.Query().Get("state") != "all" || r.URL.Query().Get("per_page") != "100" {
			t.Fatalf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`[{"number":7,"title":"Widget","state":"closed","draft":true,"merged_at":"2026-07-01T00:00:00Z","html_url":"https://github.com/acme/widget/pull/7","created_at":"2026-06-01T00:00:00Z","head":{"ref":"TASK-7-widget","sha":"deadbeef","repo":{"full_name":"fork/widget"}}}]`))
	}))
	defer server.Close()
	client, err := New(Options{Token: "token", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	prs, err := client.ListRepositoryPRs("acme", "widget")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].State != "MERGED" || prs[0].HeadRepo != "fork/widget" || prs[0].HeadSHA != "deadbeef" {
		t.Fatalf("PRs = %+v", prs)
	}
}

func TestFetchPRStatusSelectsOpenExactBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"number":1,"state":"closed","created_at":"2026-07-02T00:00:00Z","head":{"ref":"feature/x","repo":{"full_name":"acme/widget"}}},
			{"number":2,"state":"open","created_at":"2026-07-01T00:00:00Z","head":{"ref":"feature/x","repo":{"full_name":"acme/widget"}}},
			{"number":3,"state":"open","created_at":"2026-07-03T00:00:00Z","head":{"ref":"other","repo":{"full_name":"acme/widget"}}}
		]`))
	}))
	defer server.Close()
	client, _ := New(Options{Token: "token", BaseURL: server.URL, HTTPClient: server.Client()})
	status, err := client.FetchPRStatus("feature/x", "acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	if status.Number != 2 {
		t.Fatalf("PR = %+v, want #2", status)
	}
}

func TestFetchPRsForCommitUsesExactEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/commits/abc123/pulls" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"number":9,"state":"open","head":{"ref":"feature/9","repo":{"full_name":"acme/widget"}}}]`))
	}))
	defer server.Close()
	client, _ := New(Options{Token: "token", BaseURL: server.URL, HTTPClient: server.Client()})
	status, err := client.FetchPRsForCommit("acme", "widget", "abc123")
	if err != nil || status.Number != 9 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestAPIErrorsAreClassifiedAndDoNotExposeToken(t *testing.T) {
	reset := time.Now().Add(time.Hour).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()
	client, _ := New(Options{Token: "super-secret", BaseURL: server.URL, HTTPClient: server.Client()})
	_, err := client.ListRepositoryPRs("acme", "widget")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrorRateLimit || apiErr.ResetAt.IsZero() {
		t.Fatalf("error = %#v", err)
	}
	if err != nil && contains(err.Error(), "super-secret") {
		t.Fatalf("error exposed token: %v", err)
	}
}

func TestDetectRepoAndBranchAtUseExplicitDirectory(t *testing.T) {
	dir := t.TempDir()
	commands := map[string]string{
		"remote get-url origin":       "git@github.com:acme/widget.git\n",
		"rev-parse --abbrev-ref HEAD": "feature/TASK-1\n",
	}
	client, err := New(Options{Token: "token", ExecCommand: helperCommand(t, dir, commands)})
	if err != nil {
		t.Fatal(err)
	}
	owner, repo, err := client.DetectRepoAt(dir)
	if err != nil || owner != "acme" || repo != "widget" {
		t.Fatalf("repo=%s/%s err=%v", owner, repo, err)
	}
	branch, err := client.DetectBranchAt(dir)
	if err != nil || branch != "feature/TASK-1" {
		t.Fatalf("branch=%q err=%v", branch, err)
	}
}

func helperCommand(t *testing.T, wantDir string, outputs map[string]string) func(string, ...string) *exec.Cmd {
	t.Helper()
	return func(name string, args ...string) *exec.Cmd {
		key := ""
		for i, arg := range args {
			if i > 0 {
				key += " "
			}
			key += arg
		}
		output, ok := outputs[key]
		if !ok {
			t.Fatalf("unexpected command: %s %s", name, key)
		}
		script := filepath.Join(t.TempDir(), "git")
		if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '"+output+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(script)
		cmd.Dir = wantDir
		return cmd
	}
}

func contains(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
