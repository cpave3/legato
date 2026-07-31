package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPRByNumberUsesTokenAuthenticatedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/pulls/42" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got == "" {
			t.Fatal("missing GitHub API version header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":42,"title":"Fix widget","state":"open","draft":false,"merged":false,"html_url":"https://github.com/acme/widget/pull/42","comments":2,"review_comments":3,"created_at":"2026-07-01T00:00:00Z","head":{"ref":"feature/TASK-42","sha":"abc123","repo":{"full_name":"acme/widget"}}}`))
	}))
	defer server.Close()

	client, err := New(Options{Token: "secret-token", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.FetchPRByNumber("acme", "widget", 42)
	if err != nil {
		t.Fatal(err)
	}
	if status.Number != 42 || status.HeadBranch != "feature/TASK-42" || status.HeadSHA != "abc123" || status.CommentCount != 5 {
		t.Fatalf("status = %+v", status)
	}
}
