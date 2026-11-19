package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// withDoctorReport makes legato doctor report r, recording whether it was
// asked to check online.
func withDoctorReport(t *testing.T, r service.DoctorReport) *bool {
	t.Helper()
	online := new(bool)
	old := doctorReport
	doctorReport = func(_ context.Context, asked bool) (service.DoctorReport, error) {
		*online = asked
		r.Online = asked
		return r, nil
	}
	t.Cleanup(func() { doctorReport = old })
	return online
}

func runCapturingStdout(t *testing.T, run func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()
	code := run()
	w.Close()
	os.Stdout = old
	return string(<-done), code
}

var doctorFixture = service.DoctorReport{CheckedAt: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC), Checks: []service.DoctorCheck{
	{ID: "tmux", Title: "tmux", Required: true, Result: service.DoctorOK, Detail: "tmux 3.4"},
	{ID: "yg", Title: "Yggdrasil (yg)", Result: service.DoctorMissing, Detail: "yg not found on Legato's PATH", Fix: "Install Yggdrasil: https://github.com/cpave3/yggdrasil"},
}}

func TestDoctorPrintsEachCheckWithItsFix(t *testing.T) {
	withDoctorReport(t, doctorFixture)
	out, code := runCapturingStdout(t, func() int { return runDoctorCmd(nil) })
	if code != exitOK {
		t.Fatalf("exit %d with only an optional check failing", code)
	}
	for _, want := range []string{"tmux", "tmux 3.4", "Yggdrasil (yg)", "optional", "Install Yggdrasil: https://github.com/cpave3/yggdrasil"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorExitsNonZeroWhenARequiredCheckFails(t *testing.T) {
	r := doctorFixture
	r.Checks = append([]service.DoctorCheck{}, r.Checks...)
	r.Checks[0] = service.DoctorCheck{ID: "tmux", Title: "tmux", Required: true, Result: service.DoctorMissing, Detail: "tmux not found", Fix: "Install tmux"}
	withDoctorReport(t, r)
	out, code := runCapturingStdout(t, func() int { return runDoctorCmd(nil) })
	if code != exitDependency {
		t.Fatalf("exit %d, want %d", code, exitDependency)
	}
	if !strings.Contains(out, "required") || !strings.Contains(out, "Install tmux") {
		t.Errorf("output:\n%s", out)
	}
	_, code = runCapturingStdout(t, func() int { return runDoctorCmd([]string{"--json"}) })
	if code != exitDependency {
		t.Fatalf("--json exit %d, want %d", code, exitDependency)
	}
}

func TestDoctorJSONIsTheReport(t *testing.T) {
	online := withDoctorReport(t, doctorFixture)
	out, code := runCapturingStdout(t, func() int { return runDoctorCmd([]string{"--json", "--online"}) })
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if !*online {
		t.Error("--online was not passed on")
	}
	var envelope struct {
		OK   bool                 `json:"ok"`
		Data service.DoctorReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !envelope.OK || len(envelope.Data.Checks) != 2 || envelope.Data.Checks[1].Fix == "" || !envelope.Data.Online {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestDoctorIsForwardedFromTheSandbox(t *testing.T) {
	for _, args := range [][]string{{"doctor"}, {"doctor", "--json"}, {"doctor", "--online", "--json"}} {
		if !forwardable(args) {
			t.Errorf("%v is not forwarded", args)
		}
	}
	if forwardable([]string{"doctor", "--help"}) {
		t.Error("doctor --help is forwarded")
	}
}

func TestDoctorRequiresWhatTheSettingsUse(t *testing.T) {
	cfg := &config.Config{}
	cfg.Swarm.Worker.Agent = "codex"
	cfg.Agents.Sandbox.Enabled = true
	cfg.GitHub.Auth = "gh"
	got := doctorConfig(cfg, []*workflow.Definition{{Name: "cleanup", Steps: []workflow.Step{{Type: "worktree.remove"}}}})
	if got.DefaultAgent != "codex" || !got.SandboxEnabled {
		t.Errorf("config = %+v", got)
	}
	for _, id := range []string{"gh", "gh.auth", "yg"} {
		if got.RequiredBy[id] == "" {
			t.Errorf("%s not required: %+v", id, got.RequiredBy)
		}
	}
	if plain := doctorConfig(&config.Config{}, nil); len(plain.RequiredBy) != 0 {
		t.Errorf("defaults require %+v", plain.RequiredBy)
	}
}
