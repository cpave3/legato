package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skippedNotice is the row Claude Code writes into a session's transcript
// when its security mod passes one of a plugin's hooks over (LEG-684).
func skippedNotice(plugin, event string, at time.Time) string {
	return `{"type":"system","subtype":"informational","level":"notice","content":"` + plugin + `: its ` + event +
		` hook was skipped. \"cc-plugin-sec-default\" runs first and sent the event on past it.","timestamp":"` +
		at.UTC().Format(time.RFC3339Nano) + `","version":"2.1.332"}`
}

func writeTranscript(t *testing.T, dir, name string, modified time.Time, rows ...string) {
	t.Helper()
	path := filepath.Join(dir, "projects", "-work-repo", name+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func writeInstalledMod(t *testing.T, modDir string, at time.Time) {
	t.Helper()
	path := filepath.Join(modDir, "legato", "hooks", "register.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// mod"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

const userRow = `{"type":"user","message":{"role":"user","content":"hi"}}`

func TestClaudeHooksCheckWarnsWhenClaudeCodeSkipsLegatosHooks(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	claude, modDir := t.TempDir(), t.TempDir()
	writeInstalledMod(t, modDir, now.Add(-48*time.Hour))
	writeTranscript(t, claude, "s-1", now.Add(-time.Hour), userRow,
		skippedNotice("legato", "classic.PermissionRequest", now.Add(-time.Hour)),
		skippedNotice("probe", "classic.PermissionRequest", now.Add(-time.Hour)))

	check := checkClaudeHooks([]string{claude}, modDir, now)

	if check.ID != "claude-code.hooks" || check.Required {
		t.Fatalf("check = %+v", check)
	}
	if check.Result != DoctorFailed || !strings.Contains(check.Detail, "legato: classic.PermissionRequest") || !strings.Contains(check.Detail, "cc-plugin-sec-default") {
		t.Fatalf("check = %+v", check)
	}
	if strings.Contains(check.Detail, "probe") || check.Fix == "" {
		t.Fatalf("check = %+v", check)
	}
}

// Notices an older Legato's mod left behind say nothing of this one: only
// those recorded since the installed mod was written count.
func TestClaudeHooksCheckIgnoresWhatAnOlderModLeftBehind(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	claude, modDir := t.TempDir(), t.TempDir()
	writeInstalledMod(t, modDir, now.Add(-2*time.Hour))
	writeTranscript(t, claude, "s-1", now.Add(-time.Hour), userRow,
		skippedNotice("legato", "classic.PermissionRequest", now.Add(-3*time.Hour)))
	// Older than a week: not read at all.
	writeTranscript(t, claude, "s-2", now.Add(-8*24*time.Hour),
		skippedNotice("legato-state", "classic.Stop", now.Add(-time.Hour)))

	if check := checkClaudeHooks([]string{claude}, modDir, now); check.Result != DoctorOK {
		t.Fatalf("check = %+v", check)
	}
}

func TestClaudeHooksCheckIsSkippedWithoutRecentSessions(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

	check := checkClaudeHooks([]string{t.TempDir(), filepath.Join(t.TempDir(), "missing")}, "", now)

	if check.Result != DoctorSkipped || check.Failed() {
		t.Fatalf("check = %+v", check)
	}
}

func TestDoctorRunsTheClaudeHooksCheckWhereTranscriptsAreKept(t *testing.T) {
	claude := t.TempDir()
	writeTranscript(t, claude, "s-1", time.Now(), userRow, skippedNotice("legato", "classic.StopFailure", time.Now()))

	r := runDoctor(t, healthyMachine(), DoctorConfig{DefaultAgent: "claude-code", ClaudeConfigDirs: []string{claude}}, false)

	check := findCheck(t, r, "claude-code.hooks")
	if check.Result != DoctorFailed || check.Required || !r.OK() {
		t.Fatalf("check = %+v, report OK = %v", check, r.OK())
	}
}
