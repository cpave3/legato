package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

type pastedSecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *pastedSecrets) Put(_ context.Context, name, value string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[name] = value
	return "legato://" + name, nil
}

func TestTheReeveGetsASecretByNameAndNeverItsValue(t *testing.T) {
	approvals, db, _ := newApprovalFixture(t)
	sink := &pastedSecrets{}
	secrets := &ReeveSecrets{Approvals: approvals, Sink: sink, Wait: 3 * time.Second}
	secrets.Register()
	ctx := context.Background()
	go func() {
		for {
			pending, _ := approvals.Pending(ctx)
			if len(pending) == 1 {
				secrets.Fulfil(ctx, pending[0].ID, "sk-elevenlabs-123")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	out, err := secrets.Request(ctx, "rv1", "elevenlabs-key", "ElevenLabs for the narrated videos")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "stored" || out.Ref != "legato://elevenlabs-key" || sink.values["elevenlabs-key"] != "sk-elevenlabs-123" {
		t.Fatalf("outcome %+v, stored %v", out, sink.values)
	}
	rows, _ := db.ListApprovals(ctx, store.ApprovalFilter{})
	for _, row := range rows {
		if strings.Contains(row.DetailsJSON+row.DecisionJSON, "sk-elevenlabs-123") {
			t.Fatal("the value is in the approval's record")
		}
	}
}

func TestARequestForASecretIsNotAnsweredByAPlainAllow(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	secrets := &ReeveSecrets{Approvals: approvals, Sink: &pastedSecrets{}, Wait: time.Millisecond}
	secrets.Register()
	ctx := context.Background()
	out, _ := secrets.Request(ctx, "rv1", "gh-token", "GitHub")
	if _, err := approvals.Decide(ctx, out.ApprovalID, ApprovalDecision{Decision: ApprovalAllow, Option: "stored", By: "web"}); err == nil {
		t.Fatal("allowed without a value")
	}
}
