package service

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeWorkflowAttentionSource struct {
	runs         []store.WorkflowRun
	view         *store.WorkflowRunView
	showErr      error
	triggerCalls int
	retryCalls   int
	promptCalls  int
}

func (f *fakeWorkflowAttentionSource) ListRuns(context.Context, store.WorkflowRunFilter) ([]store.WorkflowRun, error) {
	return f.runs, nil
}
func (f *fakeWorkflowAttentionSource) Show(context.Context, string) (*store.WorkflowRunView, error) {
	return f.view, f.showErr
}
func (f *fakeWorkflowAttentionSource) TriggerStep(context.Context, string, string, bool) (*store.WorkflowRunView, error) {
	f.triggerCalls++
	return f.view, nil
}
func (f *fakeWorkflowAttentionSource) Retry(context.Context, string, string, bool) (*store.WorkflowRunView, error) {
	f.retryCalls++
	return f.view, nil
}
func (f *fakeWorkflowAttentionSource) PromptStep(context.Context, string, string) (*store.WorkflowRunView, error) {
	f.promptCalls++
	return f.view, nil
}
