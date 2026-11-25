package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// A ticket says whether its status is done, which open tickets block it, and
// its epic when Jira names it as the parent (LEG-488).
func TestATicketCarriesItsStatusCategoryBlockersAndParent(t *testing.T) {
	var fields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields = r.URL.Query().Get("fields")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"issues":[{"key":"P-2","fields":{"summary":"Two","updated":"2025-01-01T00:00:00.000+0000",
			"status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},
			"parent":{"key":"P-1","fields":{"issuetype":{"name":"Epic"}}},
			"issuelinks":[
				{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"P-7","fields":{"status":{"name":"To Do","statusCategory":{"key":"new"}}}}},
				{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"P-8","fields":{"status":{"name":"Done","statusCategory":{"key":"done"}}}}},
				{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"outwardIssue":{"key":"P-9","fields":{"status":{"name":"To Do","statusCategory":{"key":"new"}}}}},
				{"type":{"name":"Relates","inward":"relates to","outward":"relates to"},"inwardIssue":{"key":"P-6","fields":{"status":{"name":"To Do","statusCategory":{"key":"new"}}}}}
			]}}],"isLast":true}`)
	}))
	defer srv.Close()

	p := NewProvider(srv.URL, "a@b.com", "tok", 30*time.Second)
	tickets, err := p.Search(context.Background(), "project = P")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"issuelinks", "parent"} {
		if !slices.Contains(strings.Split(fields, ","), field) {
			t.Errorf("fields %q leave out %s", fields, field)
		}
	}
	got := tickets[0]
	if got.StatusCategory != "indeterminate" || strings.Join(got.BlockedBy, ",") != "P-7" || got.EpicKey != "P-1" {
		t.Fatalf("ticket = %+v", got)
	}
}

// A sub-task's parent is a story, not its epic.
func TestASubtasksParentIsNotItsEpic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"issues":[{"key":"P-3","fields":{"summary":"Sub","status":{"name":"To Do"},"parent":{"key":"P-2","fields":{"issuetype":{"name":"Story"}}}}}],"isLast":true}`)
	}))
	defer srv.Close()
	tickets, err := NewProvider(srv.URL, "a@b.com", "tok", 30*time.Second).Search(context.Background(), "project = P")
	if err != nil {
		t.Fatal(err)
	}
	if tickets[0].EpicKey != "" {
		t.Fatalf("epic = %q", tickets[0].EpicKey)
	}
}
