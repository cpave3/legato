package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func hostFixture(t *testing.T) (*changeSetFixture, *[]HostCommand) {
	t.Helper()
	f := newChangeSetFixture(t)
	var ran []HostCommand
	f.svc.opts.HostExec = func(_ context.Context, cmd HostCommand, timeout time.Duration) (int, []byte, error) {
		ran = append(ran, cmd)
		if timeout != 10*time.Minute {
			t.Errorf("timeout = %v", timeout)
		}
		if strings.Contains(strings.Join(cmd.Argv, " "), "big") {
			return 0, []byte(strings.Repeat("x", 70<<10)), nil
		}
		return 3, []byte("df: /data: no such file"), nil
	}
	f.svc.opts.OutputDir = t.TempDir()
	return f, &ran
}

func TestAHostCommandRunsOnceApprovedAndIsAudited(t *testing.T) {
	f, ran := hostFixture(t)
	ctx := context.Background()
	out, err := f.svc.Propose(ctx, f.reeve, "check the disk", []ChangeCall{{Kind: CallHostShell, Command: "df -h /data"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Level != LevelApprove {
		t.Fatalf("level = %s", out.Level)
	}
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	f.svc.RunQueued(ctx)
	if len(*ran) != 1 || strings.Join((*ran)[0].Argv, " ") != "sh -c df -h /data" || (*ran)[0].AsRoot {
		t.Fatalf("ran %+v", *ran)
	}
	if !strings.Contains(f.notes[0], "failed (3)") || !strings.Contains(f.notes[0], "no such file") {
		t.Fatalf("notes = %v", f.notes)
	}
	rows, _ := f.devices.db.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "host"})
	if len(rows) != 1 || rows[0].Operation != CallHostShell || rows[0].Resource != "df -h /data" || rows[0].DecidedBy != "web" || !strings.Contains(rows[0].Detail, `"exit":3`) || !strings.Contains(rows[0].Detail, "output_sha256") {
		t.Fatalf("audit = %+v", rows)
	}
}

func TestAnOutputPastTheCapIsKeptInAFile(t *testing.T) {
	f, _ := hostFixture(t)
	ctx := context.Background()
	out, _ := f.svc.Propose(ctx, f.reeve, "big", []ChangeCall{{Kind: CallHostShell, Command: "cat big.log"}})
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	f.svc.RunQueued(ctx)
	set, _ := f.devices.db.GetReeveChangeSet(ctx, out.ID)
	var results []CallResult
	json.Unmarshal([]byte(set.Results), &results)
	logs, _ := filepath.Glob(filepath.Join(f.svc.opts.OutputDir, "*.log"))
	if len(logs) != 1 || !strings.Contains(results[0].Detail, "x") {
		t.Fatalf("logs %v, result %+v", logs, results[0])
	}
	if data, _ := os.ReadFile(logs[0]); len(data) != 70<<10 {
		t.Fatalf("kept %d bytes", len(data))
	}
}

func TestAHostCallAsRootAlwaysNeedsASignatureWithVerification(t *testing.T) {
	f, _ := hostFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Propose(ctx, f.reeve, "apt", []ChangeCall{{Kind: CallHostShell, Command: "apt-get update", AsRoot: true}}); !errors.Is(err, ErrNoDeviceEnrolled) {
		t.Fatalf("as root with no device: %v", err)
	}
	f.devices.enroll(t, newPhone(t), "Pixel")
	out, err := f.svc.Propose(ctx, f.reeve, "apt", []ChangeCall{{Kind: CallHostShell, Command: "apt-get update", AsRoot: true}})
	if err != nil {
		t.Fatal(err)
	}
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	if out.Level != LevelVerified || !approval.Verified {
		t.Fatalf("level %s, verified %v", out.Level, approval.Verified)
	}
}

func TestHostOperationsCanBeTurnedOff(t *testing.T) {
	f, _ := hostFixture(t)
	os.WriteFile(os.Getenv("LEGATO_CONFIG"), []byte("reeve:\n  host_operations: false\n"), 0o600)
	if _, err := f.svc.Propose(context.Background(), f.reeve, "x", []ChangeCall{{Kind: CallHostShell, Command: "true"}}); !errors.Is(err, ErrHostOperationsOff) {
		t.Fatalf("with host operations off: %v", err)
	}
}

func TestAFileIsWrittenAtomicallyWithABackupAndAServiceByItsUnit(t *testing.T) {
	f, ran := hostFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "Caddyfile")
	os.WriteFile(path, []byte("old\n"), 0o640)
	out, err := f.svc.Propose(ctx, f.reeve, "proxy", []ChangeCall{
		{Kind: CallEditFile, File: path, Content: "new\n"},
		{Kind: CallManageService, Unit: "caddy.service", Action: "restart", System: true, AsRoot: true},
	})
	if !errors.Is(err, ErrNoDeviceEnrolled) {
		t.Fatalf("a root service call with no device: %v", err)
	}
	out, err = f.svc.Propose(ctx, f.reeve, "proxy", []ChangeCall{
		{Kind: CallEditFile, File: path, Content: "new\n"},
		{Kind: CallManageService, Unit: "legato.service", Action: "status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	var details ChangeSetDetails
	json.Unmarshal(approval.Details, &details)
	if !strings.Contains(details.Calls[0].Diff, "+ new") {
		t.Fatalf("diff = %q", details.Calls[0].Diff)
	}
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	f.svc.RunQueued(ctx)
	if data, _ := os.ReadFile(path); string(data) != "new\n" {
		t.Fatalf("file = %q", data)
	}
	if baks, _ := filepath.Glob(path + ".*.bak"); len(baks) != 1 {
		t.Fatalf("backups = %v", baks)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	if len(*ran) != 1 || strings.Join((*ran)[0].Argv, " ") != "systemctl --user status legato.service --no-pager" {
		t.Fatalf("ran %+v", *ran)
	}
}

// A file edit's diff is bounded: a file or content past the limit is
// refused, and a large rewrite is shown as the old lines out and the new
// in, without the work of a line-by-line match.
func TestAFileEditsDiffIsBounded(t *testing.T) {
	f, _ := hostFixture(t)
	ctx := context.Background()
	dir := t.TempDir()
	big := filepath.Join(dir, "big.log")
	os.WriteFile(big, []byte(strings.Repeat("x", MaxEditFileBytes+1)), 0o600)
	if _, err := f.svc.Propose(ctx, f.reeve, "x", []ChangeCall{{Kind: CallEditFile, File: big, Content: "y\n"}}); err == nil {
		t.Fatal("a file past the limit was diffed")
	}
	small := filepath.Join(dir, "small")
	if _, err := f.svc.Propose(ctx, f.reeve, "x", []ChangeCall{{Kind: CallEditFile, File: small, Content: strings.Repeat("y", MaxEditFileBytes+1)}}); err == nil {
		t.Fatal("content past the limit was diffed")
	}

	var old, next strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&old, "old line %d\n", i)
		fmt.Fprintf(&next, "new line %d\n", i)
	}
	lines := filepath.Join(dir, "lines")
	os.WriteFile(lines, []byte(old.String()), 0o600)
	start := time.Now()
	out, err := f.svc.Propose(ctx, f.reeve, "rewrite", []ChangeCall{{Kind: CallEditFile, File: lines, Content: next.String()}})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("describing took %v", took)
	}
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	var details ChangeSetDetails
	json.Unmarshal(approval.Details, &details)
	if !strings.Contains(details.Calls[0].Diff, "- old line 19999") || !strings.Contains(details.Calls[0].Diff, "+ new line 0") {
		t.Fatal("the rewrite's diff lacks its lines")
	}
}
