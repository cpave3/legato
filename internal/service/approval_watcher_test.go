package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeNativeStream is one agent endpoint: a snapshot, and events pushed
// into its open stream.
type fakeNativeStream struct {
	mu       sync.Mutex
	snapshot *chimera.SessionSnapshot
	events   chan chimera.EventEnvelope
	opened   chan struct{}
}

func newFakeNativeStream() *fakeNativeStream {
	return &fakeNativeStream{snapshot: &chimera.SessionSnapshot{}, events: make(chan chimera.EventEnvelope, 16), opened: make(chan struct{}, 16)}
}

func (f *fakeNativeStream) Snapshot(context.Context, string, string) (*chimera.SessionSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, nil
}

func (f *fakeNativeStream) Subscribe(ctx context.Context, _, _, _ string, onEvent func(chimera.EventEnvelope)) error {
	f.opened <- struct{}{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-f.events:
			onEvent(event)
		}
	}
}

func (f *fakeNativeStream) emit(eventType string, fields map[string]any) {
	fields["type"] = eventType
	data, _ := json.Marshal(fields)
	f.events <- chimera.EventEnvelope{EventID: eventType, Type: eventType, Data: data}
}

type recordingInteraction struct {
	mu          sync.Mutex
	permissions []PermissionAction
	answers     []QuestionAnswer
	requestIDs  []string
}

func (r *recordingInteraction) ResolvePermission(_ context.Context, _ string, requestID string, action PermissionAction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestIDs = append(r.requestIDs, requestID)
	r.permissions = append(r.permissions, action)
	return nil
}

func (r *recordingInteraction) AnswerQuestion(_ context.Context, _ string, requestID string, answer QuestionAnswer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestIDs = append(r.requestIDs, requestID)
	r.answers = append(r.answers, answer)
	return nil
}

type watcherFixture struct {
	approvals   *ApprovalService
	watcher     *ApprovalWatcher
	stream      *fakeNativeStream
	interaction *recordingInteraction
}

func newWatcherFixture(t *testing.T) watcherFixture {
	t.Helper()
	approvals, _, _ := newApprovalFixture(t)
	stream := newFakeNativeStream()
	interaction := &recordingInteraction{}
	endpoint, session := "http://127.0.0.1:1/x", "task-1"
	watcher := NewApprovalWatcher(approvals, interaction, ApprovalWatcherOptions{
		Sessions: func(context.Context) ([]store.AgentSession, error) {
			return []store.AgentSession{{TaskID: "task-1", ExecutionID: "ex-1", Status: "running", AgentKind: "claude-code", ChimeraEndpoint: &endpoint, ChimeraSessionID: &session}}, nil
		},
		Stream:     stream,
		RetryDelay: time.Millisecond,
	})
	t.Cleanup(watcher.Stop)
	return watcherFixture{approvals: approvals, watcher: watcher, stream: stream, interaction: interaction}
}

func (f watcherFixture) start(t *testing.T) {
	t.Helper()
	f.watcher.Reconcile(context.Background())
	select {
	case <-f.stream.opened:
	case <-time.After(time.Second):
		t.Fatal("the watcher did not open the agent's stream")
	}
}

// pendingSoon waits for the number of pending approvals to reach want.
func (f watcherFixture) pendingSoon(t *testing.T, want int) []Approval {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending, err := f.approvals.List(context.Background(), store.ApprovalFilter{Status: store.ApprovalPending})
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == want {
			return pending
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending approvals = %d, want %d", len(pending), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAPermissionRequestBecomesAPendingApprovalWithItsOptionsAndDeny(t *testing.T) {
	f := newWatcherFixture(t)
	f.start(t)

	f.stream.emit("permission_request", map[string]any{
		"requestId": "request-1", "tool": "Bash", "target": "host", "command": "go test ./...", "reason": "run the tests",
		"options": []map[string]string{{"id": "once", "label": "Allow once"}, {"id": "always", "label": "Allow, and don't ask again"}},
	})

	approval := f.pendingSoon(t, 1)[0]
	if approval.Source != ApprovalSourceAgentPermission || approval.SourceKey != "ex-1/task-1/request-1" || approval.TaskID != "task-1" {
		t.Fatalf("approval = %+v", approval.Approval)
	}
	if approval.Summary != "go test ./..." {
		t.Fatalf("summary = %q", approval.Summary)
	}
	var ids []string
	for _, option := range approval.Options {
		ids = append(ids, option.ID+":"+option.Kind)
	}
	if len(ids) != 3 || ids[0] != "once:allow" || ids[1] != "always:allow" || ids[2] != "deny:deny" {
		t.Fatalf("options = %v", ids)
	}
}

func TestApprovingAPermissionResolvesItAtTheAgent(t *testing.T) {
	f := newWatcherFixture(t)
	f.start(t)
	f.stream.emit("permission_request", map[string]any{"requestId": "request-1", "tool": "Bash", "target": "host", "command": "ls"})
	approval := f.pendingSoon(t, 1)[0]

	if _, err := f.approvals.Decide(context.Background(), approval.ID, ApprovalDecision{Decision: ApprovalAllow, By: "web"}); err != nil {
		t.Fatal(err)
	}
	if len(f.interaction.permissions) != 1 || f.interaction.permissions[0].Decision != "allow" || f.interaction.permissions[0].Option != "" || f.interaction.requestIDs[0] != "request-1" {
		t.Fatalf("resolved %+v %v, want a plain allow of request-1", f.interaction.permissions, f.interaction.requestIDs)
	}
}

func TestDenyingAndDismissingAPermissionDenyItWithFeedback(t *testing.T) {
	f := newWatcherFixture(t)
	f.start(t)
	f.stream.emit("permission_request", map[string]any{"requestId": "r1", "tool": "Bash", "target": "host", "command": "rm -rf build"})
	f.stream.emit("permission_request", map[string]any{"requestId": "r2", "tool": "Bash", "target": "host", "command": "make"})
	pending := f.pendingSoon(t, 2)
	ctx := context.Background()

	if _, err := f.approvals.Decide(ctx, pending[0].ID, ApprovalDecision{Decision: ApprovalDeny, Feedback: "use make clean"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.approvals.Dismiss(ctx, pending[1].ID, "", "tui"); err != nil {
		t.Fatal(err)
	}
	got := f.interaction.permissions
	if len(got) != 2 || got[0].Decision != "deny" || got[0].Feedback != "use make clean" || got[1].Decision != "deny" || got[1].Feedback != "Dismissed in Legato" {
		t.Fatalf("resolved %+v", got)
	}
}

func TestAQuestionBecomesAnApprovalAndItsAnswersReachTheAgent(t *testing.T) {
	f := newWatcherFixture(t)
	f.start(t)
	f.stream.emit("question_request", map[string]any{"requestId": "q-1", "questions": []map[string]any{
		{"id": "q1", "question": "Which database?", "options": []map[string]string{{"label": "SQLite"}, {"label": "Postgres"}}, "multiSelect": false, "allowOther": true},
	}})
	approval := f.pendingSoon(t, 1)[0]
	if approval.Source != ApprovalSourceAgentQuestion || approval.Title != "Which database?" {
		t.Fatalf("approval = %+v", approval.Approval)
	}
	ctx := context.Background()

	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAnswer, Answers: map[string][]string{"nope": {"x"}}}); err == nil {
		t.Fatal("an answer to a question the request does not ask was accepted")
	}
	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAnswer, Answers: map[string][]string{"q1": {"SQLite"}}}); err != nil {
		t.Fatal(err)
	}
	if len(f.interaction.answers) != 1 || f.interaction.answers[0].Answers["q1"][0] != "SQLite" {
		t.Fatalf("answers = %+v", f.interaction.answers)
	}
}

func TestPendingRequestsInTheSnapshotBecomeApprovals(t *testing.T) {
	f := newWatcherFixture(t)
	pending := []chimera.PendingPermission{{RequestID: "request-7", Tool: "Edit", Target: "host", Command: "edit main.go"}}
	f.stream.snapshot = &chimera.SessionSnapshot{
		PendingPermissions: &pending,
		PendingQuestions:   json.RawMessage(`[{"requestId":"q-2","questions":[{"id":"q1","question":"Ship it?","options":[{"label":"Yes"}],"multiSelect":false,"allowOther":false}]}]`),
	}
	f.start(t)

	approvals := f.pendingSoon(t, 2)
	keys := map[string]bool{approvals[0].SourceKey: true, approvals[1].SourceKey: true}
	if !keys["ex-1/task-1/request-7"] || !keys["ex-1/task-1/q-2"] {
		t.Fatalf("source keys = %v", keys)
	}
}

func TestRequestsSettledAtTheAgentAreWithdrawn(t *testing.T) {
	f := newWatcherFixture(t)
	f.start(t)
	for _, id := range []string{"r1", "r2", "r3"} {
		f.stream.emit("permission_request", map[string]any{"requestId": id, "tool": "Bash", "target": "host", "command": id})
	}
	f.stream.emit("question_request", map[string]any{"requestId": "q1", "questions": []map[string]any{{"id": "q1", "question": "?", "options": []any{}}}})
	f.pendingSoon(t, 4)

	f.stream.emit("permission_resolved", map[string]any{"requestId": "r1", "decision": "allow"})
	f.stream.emit("question_timeout", map[string]any{"requestId": "q1"})
	f.pendingSoon(t, 2)

	f.stream.emit("run_finished", map[string]any{"reason": "completed"})
	f.pendingSoon(t, 0)
}

func TestAnAgentEndingExpiresItsApprovals(t *testing.T) {
	f := newWatcherFixture(t)
	f.start(t)
	f.stream.emit("permission_request", map[string]any{"requestId": "r1", "tool": "Bash", "target": "host", "command": "ls"})
	approval := f.pendingSoon(t, 1)[0]

	f.watcher.AgentEnded(context.Background(), "task-1")

	got, _ := f.approvals.Get(context.Background(), approval.ID)
	if got.Status != store.ApprovalExpired {
		t.Fatalf("status = %s, want expired", got.Status)
	}
}

func TestListenersSeeEveryEvent(t *testing.T) {
	f := newWatcherFixture(t)
	seen := make(chan string, 4)
	f.watcher.AddListener(func(taskID string, event chimera.EventEnvelope) { seen <- taskID + ":" + event.Type })
	f.start(t)

	f.stream.emit("usage_updated", map[string]any{"usedContextTokens": 10, "contextWindow": 100})
	select {
	case got := <-seen:
		if got != "task-1:usage_updated" {
			t.Fatalf("listener saw %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("listener saw nothing")
	}
}
