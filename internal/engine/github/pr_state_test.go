package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// FetchPRState reads whether a pull request is open, closed or merged with
// one request: a step that waits for a merge asks it every few minutes.
func TestFetchPRStateReadsTheMergeWithOneRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/repos/acme/widget/pulls/42" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":42,"state":"closed","merged":true,"merged_at":"2026-11-23T10:00:00Z","html_url":"https://github.com/acme/widget/pull/42","head":{"ref":"legato/leg-7","sha":"abc123"}}`))
	}))
	defer server.Close()
	client, _ := New(Options{Token: "token", BaseURL: server.URL, HTTPClient: server.Client()})
	status, err := client.FetchPRState("acme", "widget", 42)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "MERGED" || status.MergedAt != "2026-11-23T10:00:00Z" || status.URL != "https://github.com/acme/widget/pull/42" || requests != 1 {
		t.Fatalf("status = %+v after %d requests", status, requests)
	}
}

// FetchPRWaitState also says whether an open pull request conflicts with
// its base and how its CI stands: the merge's needs-you item says so
// (LEG-614).
func TestFetchPRWaitStateReadsConflictsAndCI(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/widget/pulls/42":
			_, _ = w.Write([]byte(`{"number":42,"state":"open","mergeable":false,"mergeable_state":"dirty","html_url":"https://github.com/acme/widget/pull/42","head":{"ref":"legato/leg-7","sha":"abc123"},"base":{"ref":"main"}}`))
		case "/repos/acme/widget/commits/abc123/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"failure"}]}`))
		case "/repos/acme/widget/commits/abc123/status":
			_, _ = w.Write([]byte(`{"statuses":[]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, _ := New(Options{Token: "token", BaseURL: server.URL, HTTPClient: server.Client()})
	status, err := client.FetchPRWaitState("acme", "widget", 42)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "OPEN" || status.Mergeable == nil || *status.Mergeable || status.MergeableState != "dirty" || status.CheckStatus != "fail" || status.BaseBranch != "main" || status.HeadBranch != "legato/leg-7" {
		t.Fatalf("status = %+v", status)
	}
}

// A merged pull request needs no CI read.
func TestFetchPRWaitStateReadsNoChecksOnceMerged(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":42,"state":"closed","merged":true,"mergeable":null,"head":{"ref":"legato/leg-7","sha":"abc123"}}`))
	}))
	defer server.Close()
	client, _ := New(Options{Token: "token", BaseURL: server.URL, HTTPClient: server.Client()})
	status, err := client.FetchPRWaitState("acme", "widget", 42)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "MERGED" || status.Mergeable != nil || requests != 1 {
		t.Fatalf("status = %+v after %d requests", status, requests)
	}
}
