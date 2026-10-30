package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

type recordingMessenger struct {
	mu       sync.Mutex
	messages []string
}

func (m *recordingMessenger) QueueMessage(_ context.Context, taskID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, taskID+": "+message)
	return nil
}

func (m *recordingMessenger) all() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.messages...)
}

type brokerFixture struct {
	broker    *CredentialBroker
	approvals *ApprovalService
	store     *store.Store
	messenger *recordingMessenger
}

func rule(approve string, ops ...string) config.CredentialRule {
	return config.CredentialRule{Operation: ops, Approve: approve}
}

func newBrokerFixture(t *testing.T, credentials ...config.CredentialConfig) brokerFixture {
	t.Helper()
	approvals, db, _ := newApprovalFixture(t)
	approvals.opts.PollInterval = 5 * time.Millisecond
	ctx := context.Background()
	createTask(t, db, "task-1")
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", AgentKind: "claude-code", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BROKER_TEST_KEY", "sk-eleven")
	messenger := &recordingMessenger{}
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{
		Credentials: credentials,
		Messenger:   messenger,
		WaitTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.RegisterResolver()
	return brokerFixture{broker: broker, approvals: approvals, store: db, messenger: messenger}
}

func eleven(rules ...config.CredentialRule) config.CredentialConfig {
	return config.CredentialConfig{Name: "eleven", Secret: "env://BROKER_TEST_KEY", Profile: "secret", Policy: rules}
}

func request(ops []string, resources ...string) AccessRequest {
	return AccessRequest{TaskID: "task-1", ExecutionID: "ex-1", Credential: "eleven", Operations: ops, Resources: resources, Reason: "make the narration", Blocking: true}
}

func use(op, resource string) CredentialUse {
	return CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "eleven", Operation: op, Resource: resource}
}

func auditEvents(t *testing.T, db *store.Store) []string {
	t.Helper()
	entries, err := db.ListCredentialAudit(context.Background(), store.CredentialAuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(entries) - 1; i >= 0; i-- {
		out = append(out, entries[i].Event)
		if strings.Contains(entries[i].Detail+entries[i].Resource+entries[i].Amount, "sk-eleven") {
			t.Fatalf("the audit log holds the secret: %+v", entries[i])
		}
	}
	return out
}

func TestAnAutoRuleGrantsAtOnceAndTheValueComesWithAUse(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("auto", "read")))
	ctx := context.Background()

	result, err := f.broker.Request(ctx, request([]string{"read"}))
	if err != nil || result.Status != AccessGranted {
		t.Fatalf("result = %+v, %v", result, err)
	}
	secret, decision, err := f.broker.Value(ctx, use("read", ""))
	if err != nil || !decision.Allowed || string(secret.Value) != "sk-eleven" {
		t.Fatalf("value = %q, %+v, %v", secret.Value, decision, err)
	}
	if got := strings.Join(auditEvents(t, f.store), ","); got != "request,grant,use" {
		t.Fatalf("audit = %s", got)
	}
}

func TestAnythingNoRuleMatchesIsRefused(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("auto", "read")))

	result, err := f.broker.Request(context.Background(), request([]string{"admin"}))
	if err != nil || result.Status != AccessDenied || !strings.Contains(result.Reason, "no rule") {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if events := auditEvents(t, f.store); events[len(events)-1] != "refuse" {
		t.Fatalf("audit = %v", events)
	}
}

func TestTheMostSpecificRuleApplies(t *testing.T) {
	f := newBrokerFixture(t, eleven(
		config.CredentialRule{Resource: "https://api.elevenlabs.io/**", Approve: "auto"},
		config.CredentialRule{Resource: "https://api.elevenlabs.io/v1/user/**", Approve: "deny"},
	))
	ctx := context.Background()

	if d, _ := f.broker.Authorize(ctx, use("read", "https://api.elevenlabs.io/v1/voices")); !d.Allowed {
		t.Fatalf("voices: %+v", d)
	}
	if d, _ := f.broker.Authorize(ctx, use("read", "https://api.elevenlabs.io/v1/user/keys")); d.Allowed {
		t.Fatalf("user keys allowed: %+v", d)
	}
}

func TestASessionRequestAsksAndTheGrantCoversItsScopeOnly(t *testing.T) {
	f := newBrokerFixture(t, eleven(config.CredentialRule{Operation: config.StringList{"push"}, Resource: "github://acme/app/refs/heads/legato/**", Approve: "session"}))
	ctx := context.Background()

	go func() {
		approval := waitForPending(t, f.approvals)
		var ids []string
		for _, o := range approval.Options {
			ids = append(ids, o.ID)
		}
		if strings.Join(ids, ",") != "session,window,once,deny" {
			t.Errorf("options = %v", ids)
		}
		if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "session", By: "web"}); err != nil {
			t.Error(err)
		}
	}()
	result, err := f.broker.Request(ctx, request([]string{"push"}, "github://acme/app/refs/heads/legato/fix"))
	if err != nil || result.Status != AccessGranted {
		t.Fatalf("result = %+v, %v", result, err)
	}

	if d, _ := f.broker.Authorize(ctx, use("push", "github://acme/app/refs/heads/legato/fix")); !d.Allowed {
		t.Fatalf("the granted branch: %+v", d)
	}
	d, _ := f.broker.Authorize(ctx, use("push", "github://acme/app/refs/heads/legato/other"))
	if d.Allowed || d.ApprovalID == "" {
		t.Fatalf("another branch: %+v; want a new request", d)
	}
	if len(f.messenger.all()) != 0 {
		t.Fatalf("a decision made while the agent waited was also messaged: %v", f.messenger.all())
	}
}

func TestARequestRuleGrantsOneUse(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("request", "read")))
	ctx := context.Background()
	go func() {
		approval := waitForPending(t, f.approvals)
		_, _ = f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once"})
	}()
	if result, _ := f.broker.Request(ctx, request([]string{"read"})); result.Status != AccessGranted {
		t.Fatalf("result = %+v", result)
	}
	if d, _ := f.broker.Authorize(ctx, use("read", "")); !d.Allowed {
		t.Fatalf("first use: %+v", d)
	}
	if d, _ := f.broker.Authorize(ctx, use("read", "")); d.Allowed {
		t.Fatalf("second use allowed by a one-use grant: %+v", d)
	}
}

func TestAnUnansweredRequestIsPendingAndTheAgentHearsTheDecisionLater(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("session", "read")))
	ctx := context.Background()

	result, err := f.broker.Request(ctx, request([]string{"read"}))
	if err != nil || result.Status != AccessPending || result.ApprovalID == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if _, err := f.approvals.Decide(ctx, result.ApprovalID, ApprovalDecision{Decision: ApprovalAllow, Option: "session"}); err != nil {
		t.Fatal(err)
	}
	messages := f.messenger.all()
	if len(messages) != 1 || !strings.HasPrefix(messages[0], "task-1: [legato approvals] "+result.ApprovalID+" approved") || !strings.Contains(messages[0], "this session") {
		t.Fatalf("messages = %v", messages)
	}
	if d, _ := f.broker.Authorize(ctx, use("read", "")); !d.Allowed {
		t.Fatalf("after the late approval: %+v", d)
	}
}

func TestADenialReachesTheAgentWithTheNote(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("session", "read")))
	ctx := context.Background()
	result, _ := f.broker.Request(ctx, request([]string{"read"}))

	if _, err := f.approvals.Decide(ctx, result.ApprovalID, ApprovalDecision{Decision: ApprovalDeny, Feedback: "use the free voice"}); err != nil {
		t.Fatal(err)
	}
	messages := f.messenger.all()
	if len(messages) != 1 || !strings.Contains(messages[0], "denied") || !strings.Contains(messages[0], "use the free voice") {
		t.Fatalf("messages = %v", messages)
	}
	if events := auditEvents(t, f.store); events[len(events)-1] != "deny" {
		t.Fatalf("audit = %v", events)
	}
}

func TestPastItsLimitARuleGivesWayToTheNextOne(t *testing.T) {
	limited := rule("auto", "speak")
	limited.Limits = []config.RuleLimit{{Meter: "requests", Count: 2, Period: 24 * time.Hour}}
	window := rule("window", "speak")
	window.WindowDuration = 30 * time.Minute
	f := newBrokerFixture(t, eleven(limited, window))
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if d, _ := f.broker.Authorize(ctx, use("speak", "")); !d.Allowed {
			t.Fatalf("use %d within the limit: %+v", i+1, d)
		}
	}
	d, _ := f.broker.Authorize(ctx, use("speak", ""))
	if d.Allowed || d.ApprovalID == "" {
		t.Fatalf("a use past the limit: %+v; want it asked", d)
	}
	approval, _ := f.approvals.Get(ctx, d.ApprovalID)
	if approval.Options[0].ID != "window" {
		t.Fatalf("options = %+v; want the window rule's", approval.Options)
	}
}

func TestGrantsEndWithTheAgent(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("auto", "read")))
	ctx := context.Background()
	if result, _ := f.broker.Request(ctx, request([]string{"read"})); result.Status != AccessGranted {
		t.Fatal("not granted")
	}
	if err := f.store.TerminalizeExecution(ctx, "ex-1", store.ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.broker.Authorize(ctx, use("read", "")); d.Allowed || !strings.Contains(d.Reason, "not running") {
		t.Fatalf("after the agent ended: %+v", d)
	}
}

func TestABroaderRequestIsMarkedSo(t *testing.T) {
	f := newBrokerFixture(t, eleven(config.CredentialRule{Resource: "github://acme/app/refs/heads/legato/**", Approve: "session"}))
	result, _ := f.broker.Request(context.Background(), request([]string{"push"}, "github://acme/app/refs/heads/legato/attention-*"))
	approval, _ := f.approvals.Get(context.Background(), result.ApprovalID)
	var details struct {
		Broader bool   `json:"broader"`
		Reason  string `json:"reason"`
	}
	_ = json.Unmarshal(approval.Details, &details)
	if !details.Broader || details.Reason != "make the narration" {
		t.Fatalf("details = %s", approval.Details)
	}
}

func TestACredentialNamingAnUnknownProfileIsRefused(t *testing.T) {
	approvals, db, _ := newApprovalFixture(t)
	_, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{Credentials: []config.CredentialConfig{{Name: "x", Secret: "env://X", Profile: "nope"}}})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v", err)
	}
}

func waitForPending(t *testing.T, approvals *ApprovalService) Approval {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, _ := approvals.Pending(context.Background())
		if len(pending) > 0 {
			return pending[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Error("no approval came")
	return Approval{}
}

var _ credential.Store = credential.EnvStore{}

func TestLiveGrantsAreListedAndCanBeEndedEarly(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("auto", "read")))
	ctx := context.Background()
	if result, _ := f.broker.Request(ctx, request([]string{"read"})); result.Status != AccessGranted {
		t.Fatal("not granted")
	}
	grants, err := f.broker.Grants(ctx)
	if err != nil || len(grants) != 1 || grants[0].Credential != "eleven" || grants[0].TaskID != "task-1" || grants[0].Operations[0] != "read" {
		t.Fatalf("grants = %+v, %v", grants, err)
	}
	if err := f.broker.EndGrant(ctx, grants[0].ID, "web"); err != nil {
		t.Fatal(err)
	}
	if grants, _ := f.broker.Grants(ctx); len(grants) != 0 {
		t.Fatalf("an ended grant is still listed: %+v", grants)
	}
	if events := auditEvents(t, f.store); events[len(events)-1] != "revoke" {
		t.Fatalf("audit = %v", events)
	}
	entries, err := f.broker.Audit(ctx, store.CredentialAuditFilter{Credential: "eleven"})
	if err != nil || len(entries) == 0 {
		t.Fatalf("audit = %d, %v", len(entries), err)
	}
}

// A channel that cannot wait (a push) is refused at once while the person
// is asked, so the agent is told of the decision however soon it comes.
func TestARequestThatDidNotWaitHearsAQuickDecision(t *testing.T) {
	for _, decision := range []ApprovalDecision{{Decision: ApprovalAllow, Option: "session"}, {Decision: ApprovalDeny, Feedback: "not that branch"}} {
		f := newBrokerFixture(t, eleven(rule("session", "read")))
		f.broker.wait = time.Hour // a waiting requester would still be waiting
		ctx := context.Background()
		d, err := f.broker.CheckUse(ctx, use("read", ""))
		if err != nil || d.Allowed || d.ApprovalID == "" {
			t.Fatalf("check = %+v, %v", d, err)
		}
		if _, err := f.approvals.Decide(ctx, d.ApprovalID, decision); err != nil {
			t.Fatal(err)
		}
		messages := f.messenger.all()
		if len(messages) != 1 || !strings.Contains(messages[0], "[legato approvals] "+d.ApprovalID) {
			t.Fatalf("%s: messages = %v", decision.Decision, messages)
		}
		if decision.Decision == ApprovalAllow && !strings.Contains(messages[0], "again") {
			t.Fatalf("an approval should say to try again: %q", messages[0])
		}
	}
}
