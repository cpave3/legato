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
