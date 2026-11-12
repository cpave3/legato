package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestTheReevesApprovalsCarryItsNameAndSayIt(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	approvals.SetReeveName(func(context.Context) ReeveName { return "Tessitura" })
	secrets := &ReeveSecrets{Approvals: approvals, Sink: &pastedSecrets{}, Wait: 1}
	ctx := context.Background()
	if _, err := secrets.Request(ctx, "rv1", "elevenlabs-key", "the voice"); err != nil {
		t.Fatal(err)
	}
	pending, _ := approvals.Pending(ctx)
	if len(pending) != 1 || pending[0].Title != "Tessitura asks for a secret: elevenlabs-key" {
		t.Fatalf("pending = %+v", pending)
	}
	var d struct {
		ReeveName string `json:"reeve_name"`
		Name      string `json:"name"`
	}
	_ = json.Unmarshal(pending[0].Details, &d)
	if d.ReeveName != "Tessitura" || d.Name != "elevenlabs-key" {
		t.Fatalf("details = %s", pending[0].Details)
	}
}

func TestAnUnnamedReevesApprovalsSayTheReeveAndCarryNoName(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	secrets := &ReeveSecrets{Approvals: approvals, Sink: &pastedSecrets{}, Wait: 1}
	ctx := context.Background()
	if _, err := secrets.Request(ctx, "rv1", "k", "p"); err != nil {
		t.Fatal(err)
	}
	pending, _ := approvals.Pending(ctx)
	if pending[0].Title != "The Reeve asks for a secret: k" || strings.Contains(string(pending[0].Details), "reeve_name") {
		t.Fatalf("title %q, details %s", pending[0].Title, pending[0].Details)
	}
}

func TestOtherApprovalsAreNotGivenTheReevesName(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	approvals.SetReeveName(func(context.Context) ReeveName { return "Tessitura" })
	a, _, err := approvals.Submit(context.Background(), commandRequest("k1"))
	if err != nil || strings.Contains(string(a.Details), "reeve_name") {
		t.Fatalf("details %s, %v", a.Details, err)
	}
}

func TestANotificationOfTheReevesApprovalSaysWhoAsksByName(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
	}))
	defer srv.Close()
	notify := ApprovalNotifications(NewNtfyNotifier(srv.URL, "topic", ""), "")
	notify(Approval{Approval: store.Approval{ID: "ap-1", Source: ApprovalSourceReeveChangeSet, TaskID: "rv1", Title: "Restart Legato"}, Details: json.RawMessage(`{"reeve_name":"Tessitura"}`)})
	notify(Approval{Approval: store.Approval{ID: "ap-2", Source: ApprovalSourceReeveSecret, TaskID: "rv1", Title: "x"}, Details: json.RawMessage(`{}`)})
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || bodies[0] != "Tessitura needs your decision" || bodies[1] != "The Reeve needs your decision" {
		t.Fatalf("bodies = %q", bodies)
	}
}

// The audit log shows who asked by the Reeve's name; the row keeps the
// Reeve's task ID as it was written.
func TestTheAuditLogShowsTheReevesRowsUnderItsName(t *testing.T) {
	approvals, db, _ := newApprovalFixture(t)
	approvals.SetReeveName(func(context.Context) ReeveName { return "Tessitura" })
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	reeveTask, _ := db.CreateReeveTask(ctx, "The Reeve")
	if _, err := db.AppendCredentialAudit(ctx, store.CredentialAuditEntry{Event: "host", TaskID: reeveTask, Operation: "host_shell"}); err != nil {
		t.Fatal(err)
	}
	rows, err := broker.AuditRows(ctx, store.CredentialAuditFilter{})
	if err != nil || len(rows) != 1 || rows[0].TaskTitle != "Tessitura" || rows[0].TaskID != reeveTask {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}
