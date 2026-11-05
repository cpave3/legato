package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestTaskShowListsEveryPullRequestInStackOrder(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "abc123", Title: "Stacked", Status: "Doing", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddTaskBranches(ctx, s, "abc123", "acme/widget", []string{"legato/abc123-a", "legato/abc123-b", "legato/abc123-c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskBranchPR(ctx, "abc123", "acme/widget", "legato/abc123-a", &store.PRMeta{PRNumber: 11, PRURL: "https://github.com/acme/widget/pull/11", State: "OPEN", CheckStatus: "fail"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskBranchPR(ctx, "abc123", "acme/widget", "legato/abc123-b", &store.PRMeta{PRNumber: 12, State: "OPEN", IsDraft: true}); err != nil {
		t.Fatal(err)
	}

	raw, err := cli.TaskShow(s, "abc123", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		PullRequests []struct {
			Branch   string `json:"branch"`
			PRNumber int    `json:"pr_number"`
			IsDraft  bool   `json:"is_draft"`
		} `json:"pull_requests"`
		TrackedBranches []struct {
			Branch string `json:"branch"`
			Source string `json:"source"`
		} `json:"tracked_branches"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.PullRequests) != 2 || got.PullRequests[0].PRNumber != 11 || got.PullRequests[1].PRNumber != 12 ||
		!got.PullRequests[1].IsDraft || got.PullRequests[1].Branch != "legato/abc123-b" {
		t.Fatalf("pull_requests = %+v", got.PullRequests)
	}
	if len(got.TrackedBranches) != 3 || got.TrackedBranches[2].Branch != "legato/abc123-c" || got.TrackedBranches[2].Source != "api" {
		t.Fatalf("tracked_branches = %+v", got.TrackedBranches)
	}

	full, err := cli.TaskShow(s, "abc123", "full")
	if err != nil {
		t.Fatal(err)
	}
	first, second := strings.Index(full, "#11"), strings.Index(full, "#12")
	if !strings.Contains(full, "**Pull requests:**") || first < 0 || second < first {
		t.Fatalf("full = %q", full)
	}
}
