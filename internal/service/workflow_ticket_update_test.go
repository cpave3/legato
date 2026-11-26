package service

import (
	"fmt"
	"strings"
	"testing"
)

// ticket.update (LEG-656): assign a ticket and move it to a status by name.

func updateStep(attempt string, with map[string]any) ResolvedWorkflowStep {
	return ResolvedWorkflowStep{RunID: "wr-t", AttemptID: attempt, StepID: "assign", With: with}
}

func warningsOf(t *testing.T, result WorkflowExecutionResult) []string {
	t.Helper()
	w, ok := result.Outputs["warnings"].([]string)
	if !ok {
		t.Fatalf("warnings = %#v; want a list", result.Outputs["warnings"])
	}
	return w
}

func TestTicketUpdateAssignsMeAndMovesByStatusName(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.issue("AG-123", "To Do", nil)
	result, err := NewTicketUpdateExecutor(db, jira.writer()).Start(ctx, updateStep("wa-1",
		map[string]any{"provider": "jira", "key": "ag-123", "assignee": "me", "status": "in progress"}))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"key": "AG-123", "url": jira.srv.URL + "/browse/AG-123", "status": "In Progress",
		"assignee": "Cameron", "changed": true, "warnings": []string{}}
	if result.Status != "succeeded" || fmt.Sprint(result.Outputs) != fmt.Sprint(want) {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if got := jira.writes(); fmt.Sprint(got) != "[PUT /rest/api/3/issue/AG-123/assignee POST /rest/api/3/issue/AG-123/transitions]" {
		t.Fatalf("writes = %v", got)
	}
	if issue := jira.issues["AG-123"]; issue.status != "In Progress" || issue.assignee == nil || issue.assignee.id != "acc-me" {
		t.Fatalf("issue = %+v", issue)
	}
}

func TestTicketUpdateLeavesATicketAlreadyThereAlone(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.issue("AG-123", "In Progress", &fakeJiraMe)
	result, err := NewTicketUpdateExecutor(db, jira.writer()).Start(ctx, updateStep("wa-1",
		map[string]any{"key": "AG-123", "assignee": "me", "status": "In Progress"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["changed"] != false || result.Outputs["status"] != "In Progress" || result.Outputs["assignee"] != "Cameron" || len(jira.writes()) != 0 {
		t.Fatalf("outputs %v, writes %v", result.Outputs, jira.writes())
	}
	if jira.count("GET /rest/api/3/issue/AG-123/transitions") != 0 {
		t.Fatal("read the transitions of an issue already there")
	}
}

func TestTicketUpdateToAnUnreachableStatusNamesTheReachableOnes(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.issue("AG-123", "To Do", nil)
	step := NewTicketUpdateExecutor(db, jira.writer())
	_, err := step.Start(ctx, updateStep("wa-1", map[string]any{"key": "AG-123", "status": "In Review"}))
	if err == nil || !strings.Contains(err.Error(), `AG-123 cannot move to "In Review" from "To Do": it can move to In Progress, Done`) {
		t.Fatalf("err = %v", err)
	}

	// optional: the assignment still happens, and the status is a warning.
	result, err := step.Start(ctx, updateStep("wa-2", map[string]any{"key": "AG-123", "assignee": "me", "status": "In Review", "optional": true}))
	if err != nil {
		t.Fatal(err)
	}
	warnings := warningsOf(t, result)
	if result.Outputs["changed"] != true || result.Outputs["status"] != "To Do" || len(warnings) != 1 || !strings.Contains(warnings[0], "In Progress, Done") {
		t.Fatalf("outputs = %v", result.Outputs)
	}
}

func TestTicketUpdateOfAMissingTicket(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.issue("AG-1", "To Do", nil)
	step := NewTicketUpdateExecutor(db, jira.writer())
	if _, err := step.Start(ctx, updateStep("wa-1", map[string]any{"key": "AG-404", "status": "Done"})); err == nil || !strings.Contains(err.Error(), "AG-404") {
		t.Fatalf("err = %v", err)
	}
	result, err := step.Start(ctx, updateStep("wa-2", map[string]any{"key": "AG-404", "status": "Done", "optional": true}))
	if err != nil {
		t.Fatal(err)
	}
	if w := warningsOf(t, result); result.Outputs["changed"] != false || len(w) != 1 || !strings.Contains(w[0], "AG-404 not found") || len(jira.writes()) != 0 {
		t.Fatalf("outputs %v, writes %v", result.Outputs, jira.writes())
	}
}

// optional is for what Jira refuses, never for an outage: on_failure and
// retries still see those.
func TestOptionalTicketUpdateStillFailsWhenJiraIsDown(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.issue("AG-1", "To Do", nil)
	jira.broken = true
	_, err := NewTicketUpdateExecutor(db, jira.writer()).Start(ctx, updateStep("wa-1", map[string]any{"key": "AG-1", "status": "Done", "optional": true}))
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
}

func TestTicketUpdateAssignsByEmail(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.users = []fakeUser{{"acc-ann", "Ann", "ann@example.com"}, {"acc-x", "X", "x@example.com"}, {"acc-x2", "X Two", "x@example.com"}}
	jira.issue("AG-1", "To Do", nil)
	step := NewTicketUpdateExecutor(db, jira.writer())

	result, err := step.Start(ctx, updateStep("wa-1", map[string]any{"key": "AG-1", "assignee": "Ann@Example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["assignee"] != "Ann" || jira.issues["AG-1"].assignee.id != "acc-ann" || result.Outputs["status"] != "To Do" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if _, err := step.Start(ctx, updateStep("wa-2", map[string]any{"key": "AG-1", "assignee": "nobody@example.com"})); err == nil || !strings.Contains(err.Error(), "no Jira user has the email nobody@example.com") {
		t.Fatalf("no match: %v", err)
	}
	if _, err := step.Start(ctx, updateStep("wa-3", map[string]any{"key": "AG-1", "assignee": "x@example.com"})); err == nil || !strings.Contains(err.Error(), "X Two") {
		t.Fatalf("two matches: %v", err)
	}
	result, err = step.Start(ctx, updateStep("wa-4", map[string]any{"key": "AG-1", "assignee": "nobody@example.com", "optional": true}))
	if w := warningsOf(t, result); err != nil || result.Outputs["changed"] != false || len(w) != 1 || result.Outputs["assignee"] != "Ann" {
		t.Fatalf("optional: %v, %v", result.Outputs, err)
	}
}

func TestTicketUpdateAssignsByAccountIDAndUnassignsWithNone(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.users = []fakeUser{{"acc-ann", "Ann", "ann@example.com"}}
	jira.issue("AG-1", "To Do", nil)
	step := NewTicketUpdateExecutor(db, jira.writer())
	result, err := step.Start(ctx, updateStep("wa-1", map[string]any{"key": "AG-1", "assignee": "acc-ann"}))
	if err != nil || result.Outputs["assignee"] != "Ann" || result.Outputs["changed"] != true {
		t.Fatalf("outputs %v, %v", result.Outputs, err)
	}
	result, err = step.Start(ctx, updateStep("wa-2", map[string]any{"key": "AG-1", "assignee": "None"}))
	if err != nil || result.Outputs["assignee"] != "" || result.Outputs["changed"] != true || jira.issues["AG-1"].assignee != nil {
		t.Fatalf("outputs %v, %v", result.Outputs, err)
	}
	// Unassigned already: nothing to do.
	result, err = step.Start(ctx, updateStep("wa-3", map[string]any{"key": "AG-1", "assignee": "none"}))
	if err != nil || result.Outputs["changed"] != false || jira.count("PUT /rest/api/3/issue/AG-1/assignee") != 2 {
		t.Fatalf("outputs %v, %v", result.Outputs, err)
	}
}

func TestTicketUpdateAsksJiraWhoIsMeOncePerRun(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.issue("AG-1", "To Do", nil)
	jira.issue("AG-2", "To Do", nil)
	step := NewTicketUpdateExecutor(db, jira.writer())
	for i, key := range []string{"AG-1", "AG-2"} {
		if _, err := step.Start(ctx, updateStep(fmt.Sprintf("wa-%d", i), map[string]any{"key": key, "assignee": "me"})); err != nil {
			t.Fatal(err)
		}
	}
	if n := jira.count("GET /rest/api/3/myself"); n != 1 {
		t.Fatalf("asked /myself %d times", n)
	}
	other := updateStep("wa-9", map[string]any{"key": "AG-1", "assignee": "me"})
	other.RunID = "wr-other"
	if _, err := step.Start(ctx, other); err != nil {
		t.Fatal(err)
	}
	if n := jira.count("GET /rest/api/3/myself"); n != 2 {
		t.Fatalf("asked /myself %d times across two runs", n)
	}
}

func TestTicketUpdateChecksItsFieldsBeforeCallingAnything(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	bl := newFakeBurnliteWrites(t)
	step := NewTicketUpdateExecutor(db, jira.writer(), bl.writer())
	for _, tc := range []struct {
		name string
		with map[string]any
		want string
	}{
		{"no key", map[string]any{"provider": "jira", "status": "Done"}, "key is required"},
		{"nothing to change", map[string]any{"provider": "jira", "key": "AG-1"}, "assignee or status"},
		{"an unknown provider", map[string]any{"provider": "github", "key": "AG-1", "status": "Done"}, "jira or burnlite"},
		{"optional not a boolean", map[string]any{"provider": "jira", "key": "AG-1", "status": "Done", "optional": "yes"}, "optional"},
		{"a Burnlite assignee not me", map[string]any{"provider": "burnlite", "key": "LEG-1", "assignee": "ann"}, "me or none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := step.Start(ctx, updateStep("wa-1", tc.with))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want %q", err, tc.want)
			}
		})
	}
	if len(jira.requests) != 0 || len(bl.calls) != 0 {
		t.Fatalf("called Jira %v, Burnlite %v", jira.requests, bl.calls)
	}
}

func TestTicketUpdateClaimsAndStagesABurnliteTask(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	bl.task("LEG-9", map[string]any{"state": "open", "stage": "ready"})
	step := NewTicketUpdateExecutor(db, bl.writer())
	result, err := step.Start(ctx, updateStep("wa-1", map[string]any{"key": "LEG-9", "assignee": "me", "status": "Building"}))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"key": "LEG-9", "url": bl.srv.URL + "/LEG-9", "status": "building", "assignee": "legato", "changed": true, "warnings": []string{}}
	if fmt.Sprint(result.Outputs) != fmt.Sprint(want) {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if _, args := bl.called("claim_task"); args["holder"] != "legato" {
		t.Fatalf("claim = %v", args)
	}
	if _, args := bl.called("update_task"); args["stage"] != "building" {
		t.Fatalf("update = %v", args)
	}
	// Already there: no writes.
	result, err = step.Start(ctx, updateStep("wa-2", map[string]any{"key": "LEG-9", "assignee": "me", "status": "building"}))
	if err != nil || result.Outputs["changed"] != false || len(bl.writes()) != 2 {
		t.Fatalf("again: %v, %v, writes %v", result.Outputs, err, bl.writes())
	}
}

func TestTicketUpdateClosesABurnliteTaskForDoneOrDropped(t *testing.T) {
	for _, tc := range []struct{ status, reason, stage string }{{"done", "completed", "done"}, {"Closed", "completed", "done"}, {"dropped", "not_planned", "dropped"}} {
		t.Run(tc.status, func(t *testing.T) {
			db, ctx := ticketRun(t)
			bl := newFakeBurnliteWrites(t)
			bl.task("LEG-9", map[string]any{"state": "open", "stage": "building"})
			result, err := NewTicketUpdateExecutor(db, bl.writer()).Start(ctx, updateStep("wa-1", map[string]any{"key": "LEG-9", "status": tc.status}))
			if err != nil {
				t.Fatal(err)
			}
			if _, args := bl.called("close_task"); args["reason"] != tc.reason || result.Outputs["status"] != tc.stage || result.Outputs["changed"] != true {
				t.Fatalf("close %v, outputs %v", args, result.Outputs)
			}
		})
	}
}

func TestTicketUpdateRefusesABurnliteStatusItHasNot(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	bl.task("LEG-9", map[string]any{"state": "open", "stage": "ready"})
	bl.task("LEG-10", map[string]any{"state": "closed", "stage": "done", "closeReason": "completed"})
	step := NewTicketUpdateExecutor(db, bl.writer())
	_, err := step.Start(ctx, updateStep("wa-1", map[string]any{"key": "LEG-9", "status": "In Progress"}))
	if err == nil || !strings.Contains(err.Error(), `Burnlite has no status "In Progress": give one of idea, exploring, ready, building, done, dropped`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := step.Start(ctx, updateStep("wa-2", map[string]any{"key": "LEG-10", "status": "building"})); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed: %v", err)
	}
	if result, err := step.Start(ctx, updateStep("wa-3", map[string]any{"key": "LEG-10", "status": "done"})); err != nil || result.Outputs["changed"] != false {
		t.Fatalf("closed and done: %v, %v", result.Outputs, err)
	}
	if len(bl.writes()) != 0 {
		t.Fatalf("writes = %v", bl.writes())
	}
}

func TestTicketUpdateOfATaskClaimedByAnother(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	bl.task("LEG-9", map[string]any{"state": "open", "stage": "building", "claim": map[string]any{"holder": "LEG-12"}})
	bl.refuseTool = map[string]string{"claim_task": "already_claimed"}
	step := NewTicketUpdateExecutor(db, bl.writer())
	if _, err := step.Start(ctx, updateStep("wa-1", map[string]any{"key": "LEG-9", "assignee": "me"})); err == nil || !strings.Contains(err.Error(), "already_claimed") {
		t.Fatalf("err = %v", err)
	}
	result, err := step.Start(ctx, updateStep("wa-2", map[string]any{"key": "LEG-9", "assignee": "me", "optional": true}))
	if w := warningsOf(t, result); err != nil || result.Outputs["changed"] != false || result.Outputs["assignee"] != "LEG-12" || len(w) != 1 {
		t.Fatalf("optional: %v, %v", result.Outputs, err)
	}
	// none releases whoever holds it.
	bl.refuseTool = nil
	result, err = step.Start(ctx, updateStep("wa-3", map[string]any{"key": "LEG-9", "assignee": "none"}))
	if _, args := bl.called("release_claim"); err != nil || args["holder"] != "LEG-12" || result.Outputs["assignee"] != "" {
		t.Fatalf("release %v: %v, %v", args, result.Outputs, err)
	}
}
