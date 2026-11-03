package service

import (
	"context"
	"strings"
	"testing"
)

// A find-out delegation's agent may read, but its pushes and pull request
// and issue writes are refused whatever the rules say.
func TestAReadOnlyExecutionCannotPushOrWritePullRequestsOrIssues(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("auto", "git.read", "git.push", "git.delete-ref", "pr.create", "pr.write", "pr.ready", "issue.write")))
	ctx := context.Background()
	if err := f.store.SetExecutionReadOnly(ctx, "ex-1"); err != nil {
		t.Fatal(err)
	}
	if d, err := f.broker.Authorize(ctx, use("git.read", "")); err != nil || !d.Allowed {
		t.Fatalf("read = %+v, %v", d, err)
	}
	for _, op := range []string{"git.push", "git.push-default", "git.delete-ref", "pr.create", "pr.write", "pr.ready", "issue.write"} {
		d, err := f.broker.Authorize(ctx, use(op, ""))
		if err != nil || d.Allowed || !strings.Contains(d.Reason, "may only look") {
			t.Errorf("authorize %s = %+v, %v", op, d, err)
		}
		d, err = f.broker.CheckUse(ctx, use(op, ""))
		if err != nil || d.Allowed {
			t.Errorf("check %s = %+v, %v", op, d, err)
		}
	}
	result, err := f.broker.Request(ctx, request([]string{"git.push"}))
	if err != nil || result.Status != AccessDenied || !strings.Contains(result.Reason, "may only look") {
		t.Fatalf("request = %+v, %v", result, err)
	}
}
