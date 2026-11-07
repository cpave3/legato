package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

type recordingReviews struct {
	taskID, executionID string
	got                 service.ReviewSubmission
}

func (r *recordingReviews) Submit(_ context.Context, taskID, executionID string, sub service.ReviewSubmission) (service.ReviewSubmitResult, error) {
	r.taskID, r.executionID, r.got = taskID, executionID, sub
	return service.ReviewSubmitResult{Verdict: "changes", Message: "Accepted: the verdict is changes."}, nil
}

func toolNames(t *testing.T, opts cli.MCPOptions) string {
	t.Helper()
	listed, err := connectMCP(t, opts).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	return strings.Join(names, ",")
}

func TestSubmitReviewIsOfferedToTheReviewerOnly(t *testing.T) {
	s := newTestStore(t)
	author := seedMCPAgent(t, s, "task1")
	reviewer := seedReviewer(t, s, "task1")
	reviews := &recordingReviews{}

	if names := toolNames(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: author, Reviews: reviews}); strings.Contains(names, "submit_review") {
		t.Fatalf("the author is offered submit_review: %s", names)
	}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: reviewer, Reviews: reviews})
	result := callMCP(t, session, "submit_review", map[string]any{
		"summary":  "One bug.",
		"findings": []any{map[string]any{"pr": 7, "severity": "blocking", "title": "No bound", "body": "Retries forever.", "place": map[string]any{"path": "login.go", "line": 20, "side": "right"}}},
		"rulings":  []any{map[string]any{"finding": "f-1", "status": "resolved"}},
	})

	if result.IsError || !strings.Contains(mcpText(result), "verdict is changes") {
		t.Fatalf("submit_review = %q (error %v)", mcpText(result), result.IsError)
	}
	got := reviews.got
	if reviews.taskID != "task1" || reviews.executionID != reviewer || got.Summary != "One bug." || len(got.Findings) != 1 ||
		got.Findings[0].Place == nil || got.Findings[0].Place.Side != "RIGHT" || len(got.Rulings) != 1 || got.Rulings[0].Finding != "f-1" {
		t.Fatalf("submitted %+v by %s/%s", got, reviews.taskID, reviews.executionID)
	}
}
