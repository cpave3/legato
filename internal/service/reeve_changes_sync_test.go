package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// notesNow is what the Reeve has been sent so far.
func (f *changeSetFixture) notesNow() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.notes...)
}

func TestAnAutoSetThatFinishesInTimeAnswersItsResultsInTheToolCall(t *testing.T) {
	f := newChangeSetFixture(t)
	f.setting(t, "reeve.levels.change", `"auto"`)
	ctx := context.Background()
	out, err := f.svc.Propose(ctx, f.reeve, "Rename the task", []ChangeCall{{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1", Body: json.RawMessage(`{"title":"x"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "done" || out.Level != LevelAuto {
		t.Fatalf("outcome = %+v, want done at auto", out)
	}
	if !strings.Contains(out.Report, out.ID) || !strings.Contains(out.Report, "1 of 1 ran") || strings.HasPrefix(out.Report, "[legato") {
		t.Fatalf("report = %q", out.Report)
	}
	if len(out.Results) != 1 || out.Results[0].State != "ran" {
		t.Fatalf("results = %+v", out.Results)
	}
	if notes := f.notesNow(); len(notes) != 0 {
		t.Fatalf("the Reeve was also sent a message: %v", notes)
	}
	set, _ := f.devices.db.GetReeveChangeSet(ctx, out.ID)
	if set.State != store.ChangeSetDone {
		t.Fatalf("set = %+v", set)
	}
	// The runner does not run it a second time.
	if f.svc.RunQueued(ctx); len(f.ran) != 1 {
		t.Fatalf("ran %v", f.ran)
	}
}

func TestAnAutoSetThatFailsAnswersWhyInTheToolCall(t *testing.T) {
	f := newChangeSetFixture(t)
	f.setting(t, "reeve.levels.change", `"auto"`)
	f.failOn = "/api/tasks/T-1"
	out, err := f.svc.Propose(context.Background(), f.reeve, "Rename the task", []ChangeCall{
		{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"},
		{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "failed" || !strings.Contains(out.Report, "no such column") || !strings.Contains(out.Report, "1 failed, 1 skipped") {
		t.Fatalf("outcome = %+v", out)
	}
	if notes := f.notesNow(); len(notes) != 0 {
		t.Fatalf("notes = %v", notes)
	}
}

func TestAnAutoSetPastTheBoundIsStillRunningAndReportedByMessage(t *testing.T) {
	f := newChangeSetFixture(t)
	f.setting(t, "reeve.levels.change", `"auto"`)
	release := make(chan struct{})
	f.svc.opts.ResultWait = 20 * time.Millisecond
	f.svc.opts.Run = func(_ context.Context, method, path string, _ []byte) (int, string, error) {
		<-release
		return 200, `{"ok":true}`, nil
	}
	out, err := f.svc.Propose(context.Background(), f.reeve, "slow", []ChangeCall{{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "running" || out.Report != "" {
		t.Fatalf("outcome = %+v, want running", out)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for len(f.notesNow()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	notes := f.notesNow()
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "[legato change set] ") || !strings.Contains(notes[0], "1 of 1 ran") {
		t.Fatalf("notes = %v", notes)
	}
}

func TestTheWaitIsLongerForAHostCallAndNeverPastTheHostTimeout(t *testing.T) {
	f := newChangeSetFixture(t)
	api := []ProposedCall{{ChangeCall: ChangeCall{Kind: CallAPI}}}
	host := []ProposedCall{{ChangeCall: ChangeCall{Kind: CallAPI}}, {ChangeCall: ChangeCall{Kind: CallHostShell}}}
	if got := f.svc.resultWait(api, 10*time.Minute); got != 30*time.Second {
		t.Errorf("api wait = %v", got)
	}
	if got := f.svc.resultWait(host, 10*time.Minute); got != 90*time.Second {
		t.Errorf("host wait = %v", got)
	}
	if got := f.svc.resultWait(host, 5*time.Second); got != 5*time.Second+hostOutputGrace {
		t.Errorf("host wait under a short timeout = %v", got)
	}
}

func TestAnAutoSetWithARestartIsNotWaitedFor(t *testing.T) {
	f := newChangeSetFixture(t)
	f.setting(t, "reeve.levels.change", `"auto"`)
	f.setting(t, "reeve.levels.risky", `"auto"`)
	ctx := context.Background()
	out, err := f.svc.Propose(ctx, f.reeve, "restart", []ChangeCall{{Kind: CallRestart}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Level != LevelAuto || out.State != "approved" {
		t.Fatalf("outcome = %+v, want approved (results by message)", out)
	}
	set, _ := f.devices.db.GetReeveChangeSet(ctx, out.ID)
	if set.State != store.ChangeSetQueued {
		t.Fatalf("set = %+v", set)
	}
}

func TestASetTheOperatorApprovesIsStillReportedAsATaggedNotice(t *testing.T) {
	f := newChangeSetFixture(t)
	ctx := context.Background()
	out, _ := f.svc.Propose(ctx, f.reeve, "Rename the task", []ChangeCall{{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"}})
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	f.svc.RunQueued(ctx)
	notes := f.notesNow()
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "[legato change set] Change set "+out.ID) {
		t.Fatalf("notes = %v", notes)
	}

	out, _ = f.svc.Propose(ctx, f.reeve, "Rename again", []ChangeCall{{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"}})
	f.decide(t, out.ApprovalID, ApprovalDeny, "not now")
	notes = f.notesNow()
	if len(notes) != 2 || !strings.HasPrefix(notes[1], "[legato change set] ") || !strings.Contains(notes[1], "not now") {
		t.Fatalf("notes = %v", notes)
	}
}

func TestALongHostOutputIsCutToItsFirstAndLastLinesWithTheRestInAFile(t *testing.T) {
	f, _ := hostFixture(t)
	f.setting(t, "reeve.host_operations_auto", `true`)
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	whole := strings.Join(lines, "\n")
	f.svc.opts.HostExec = func(context.Context, HostCommand, time.Duration) (int, []byte, error) {
		return 0, []byte(whole), nil
	}
	out, err := f.svc.Propose(context.Background(), f.reeve, "read skills", []ChangeCall{{Kind: CallHostShell, Command: "cat a b c"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "done" {
		t.Fatalf("outcome = %+v", out)
	}
	detail := out.Results[0].Detail
	for _, want := range []string{"line 1\n", "line 20\n", "line 181", "line 200", "160 lines cut"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail lacks %q:\n%s", want, detail)
		}
	}
	for _, unwanted := range []string{"line 21\n", "line 100\n", "line 180\n"} {
		if strings.Contains(detail, unwanted) {
			t.Errorf("detail has %q", unwanted)
		}
	}
	logs, _ := filepath.Glob(filepath.Join(f.svc.opts.OutputDir, "*.log"))
	if len(logs) != 1 || !strings.Contains(detail, logs[0]) {
		t.Fatalf("logs %v, detail %q", logs, detail)
	}
	if data, _ := os.ReadFile(logs[0]); string(data) != whole {
		t.Fatalf("kept %d bytes", len(data))
	}
	if !strings.Contains(out.Report, logs[0]) {
		t.Fatalf("report = %q", out.Report)
	}
}

func TestAShortHostOutputIsGivenWhole(t *testing.T) {
	text := "a\nb\nc"
	if got, cut := outputExcerpt([]byte(text), 64<<10); got != text || cut {
		t.Fatalf("excerpt = %q, %v", got, cut)
	}
}

func TestAChangeSetsMessageIsSummedUpInOneLine(t *testing.T) {
	for text, want := range map[string]string{
		ChangeSetTag + " Change set cs-816fab12cd (read): 1 of 1 ran.\n1. Run on the host: cat a: ran: hi\n":                                         "cs-816f… · 1 of 1 ran ✓",
		ChangeSetTag + " Change set cs-816fab12cd (edit): 1 of 3 ran, 1 failed, 1 skipped.\n":                                                        "cs-816f… · 1 of 3 ran, 1 failed, 1 skipped",
		ChangeSetTag + " Change set cs-816fab12cd (restart): 1 of 1 ran.\n1. Restart: ran\nLegato restarts now; you will be told when it is back.\n": "cs-816f… · 1 of 1 ran ✓ · restarting",
		ChangeSetTag + " The operator denied change set cs-816fab12cd (edit). Their note: no\n":                                                      "cs-816f… · denied",
		RestartTag + " Legato is back: running v1 since 10:00:00 UTC.\nThe restart was asked for by you\n":                                           "restart: back",
		RestartTag + " Legato is back: running v1 since 10:00:00 UTC.\nThe restart failed and was rolled back: bad\n":                                "restart: rolled back",
	} {
		label, summary, ok := LegatoEvent(text)
		if !ok || summary != want || (label != "change set" && label != "restart") {
			t.Errorf("%q = %q %q %v, want %q", text, label, summary, ok, want)
		}
	}
	if _, _, ok := LegatoEvent("[legato coordination] Agents here."); ok {
		t.Error("another kind of message is not an event")
	}
	if _, _, ok := LegatoEvent("please run " + ChangeSetTag); ok {
		t.Error("what the operator typed is not an event")
	}
}
