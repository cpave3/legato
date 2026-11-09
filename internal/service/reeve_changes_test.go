package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// classify stands in for the route class table.
func classify(method, path string) (string, string) {
	switch {
	case method == "GET":
		return "read", "Read " + path
	case strings.HasPrefix(path, "/api/approvals/"):
		return "never", "Decide an approval"
	case path == "/api/restart" || strings.HasPrefix(path, "/api/config/settings/agents.sandbox"):
		return "risky", "Risky " + path
	}
	return "change", "Change " + path
}

type changeSetFixture struct {
	svc      *ChangeSetService
	devices  devicesFixture
	reeve    string
	mu       sync.Mutex
	ran      []string
	notes    []string
	restarts int
	failOn   string
}

func newChangeSetFixture(t *testing.T) *changeSetFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(""), 0o600)
	t.Setenv("LEGATO_CONFIG", path)
	f := &changeSetFixture{devices: newDevicesFixture(t)}
	db := f.devices.db
	f.reeve, _ = db.CreateReeveTask(context.Background(), "The Reeve")
	f.svc = NewChangeSetService(ChangeSetOptions{
		Store: db, Approvals: f.devices.approvals, Settings: NewSettingsService(db, nil), Classify: classify,
		Run: func(_ context.Context, method, path string, _ []byte) (int, string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.ran = append(f.ran, method+" "+path)
			if path == f.failOn {
				return 409, `{"error":"no such column"}`, nil
			}
			return 200, `{"ok":true}`, nil
		},
		Restart: func() { f.restarts++ },
		Notify: func(_ context.Context, _ string, text string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.notes = append(f.notes, text)
		},
		RunningAgents: func(context.Context) []string { return []string{"T-1", "T-2"} },
		Wait:          50 * time.Millisecond,
	})
	f.svc.Register()
	return f
}

func (f *changeSetFixture) decide(t *testing.T, approvalID, decision, note string) {
	t.Helper()
	if _, err := f.devices.approvals.Decide(context.Background(), approvalID, ApprovalDecision{Decision: decision, Option: map[string]string{ApprovalAllow: "allow", ApprovalDeny: "deny"}[decision], Feedback: note, By: "web"}); err != nil {
		t.Fatal(err)
	}
}

func TestAChangeSetIsOneApprovalAtItsStrictestLevelWithEveryCallInIt(t *testing.T) {
	f := newChangeSetFixture(t)
	ctx := context.Background()
	out, err := f.svc.Propose(ctx, f.reeve, "Rename the Doing column", []ChangeCall{
		{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1", Body: json.RawMessage(`{"title":"x"}`)},
		{Kind: CallAPI, Method: "PUT", Path: "/api/config/settings/agents.reviews", Body: json.RawMessage(`{"value":false}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "pending" || out.Level != LevelApprove {
		t.Fatalf("outcome = %+v", out)
	}
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	var details ChangeSetDetails
	json.Unmarshal(approval.Details, &details)
	if approval.Source != ApprovalSourceReeveChangeSet || approval.TaskID != f.reeve || len(details.Calls) != 2 || details.Reason != "Rename the Doing column" {
		t.Fatalf("approval = %+v / %+v", approval.Approval, details)
	}
	reviews := details.Calls[1]
	if reviews.Setting != "agents.reviews" || reviews.Before != true || reviews.After != false || !reviews.AppliesAtRestart {
		t.Fatalf("setting call = %+v", reviews)
	}
}

func TestANeverCallRefusesTheSet(t *testing.T) {
	f := newChangeSetFixture(t)
	_, err := f.svc.Propose(context.Background(), f.reeve, "approve it", []ChangeCall{{Kind: CallAPI, Method: "POST", Path: "/api/approvals/ap-1/decision"}})
	if !errors.Is(err, ErrNeverCall) {
		t.Fatalf("a never call: %v", err)
	}
}

func TestARiskySetNeedsASignatureOnceADeviceIsEnrolled(t *testing.T) {
	f := newChangeSetFixture(t)
	ctx := context.Background()
	out, _ := f.svc.Propose(ctx, f.reeve, "restart", []ChangeCall{{Kind: CallRestart}})
	if out.Level != LevelApprove {
		t.Fatalf("with no device: %s", out.Level)
	}
	f.devices.enroll(t, newPhone(t), "Pixel")
	out, _ = f.svc.Propose(ctx, f.reeve, "restart", []ChangeCall{{Kind: CallRestart}})
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	if out.Level != LevelSigned || !approval.Signed {
		t.Fatalf("with a device: %s, signed %v", out.Level, approval.Signed)
	}
}

// Approved, the set runs once, in order, a restart last, stopping at the
// first failure; the Reeve is told each call's result.
func TestAnApprovedSetRunsOnceInOrderAndStopsAtTheFirstFailure(t *testing.T) {
	f := newChangeSetFixture(t)
	ctx := context.Background()
	f.failOn = "/api/tasks/T-2"
	out, _ := f.svc.Propose(ctx, f.reeve, "tidy up", []ChangeCall{
		{Kind: CallRestart},
		{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"},
		{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-2"},
		{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-3"},
	})
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	if n := f.svc.RunQueued(ctx); n != 1 {
		t.Fatalf("ran %d sets", n)
	}
	if n := f.svc.RunQueued(ctx); n != 0 {
		t.Fatal("ran a set twice")
	}
	if strings.Join(f.ran, ",") != "PATCH /api/tasks/T-1,PATCH /api/tasks/T-2" || f.restarts != 0 {
		t.Fatalf("ran %v, restarts %d", f.ran, f.restarts)
	}
	if len(f.notes) != 1 || !strings.Contains(f.notes[0], "1 of 4 ran, 1 failed, 2 skipped") || !strings.Contains(f.notes[0], "no such column") {
		t.Fatalf("notes = %v", f.notes)
	}
	set, _ := f.devices.db.GetReeveChangeSet(ctx, out.ID)
	if set.State != store.ChangeSetFailed {
		t.Fatalf("state = %s", set.State)
	}
}

func TestARestartRunsLast(t *testing.T) {
	f := newChangeSetFixture(t)
	ctx := context.Background()
	out, _ := f.svc.Propose(ctx, f.reeve, "apply", []ChangeCall{{Kind: CallRestart}, {Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"}})
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	f.svc.RunQueued(ctx)
	if len(f.ran) != 1 || f.restarts != 1 || !strings.Contains(f.notes[0], "restarts now") {
		t.Fatalf("ran %v, restarts %d, notes %v", f.ran, f.restarts, f.notes)
	}
}

// A denial after propose_changes stopped waiting reaches the Reeve with the
// person's note.
func TestADenialPassesTheNoteToTheReeve(t *testing.T) {
	f := newChangeSetFixture(t)
	ctx := context.Background()
	out, _ := f.svc.Propose(ctx, f.reeve, "turn reviews off", []ChangeCall{{Kind: CallAPI, Method: "PUT", Path: "/api/config/settings/agents.reviews", Body: json.RawMessage(`{"value":false}`)}})
	f.decide(t, out.ApprovalID, ApprovalDeny, "keep reviews on")
	if len(f.notes) != 1 || !strings.Contains(f.notes[0], "keep reviews on") || !strings.Contains(f.notes[0], "Do not propose the same change again") {
		t.Fatalf("notes = %v", f.notes)
	}
	if f.svc.RunQueued(ctx) != 0 || len(f.ran) != 0 {
		t.Fatal("a denied set ran")
	}
}
