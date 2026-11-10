package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/engram"
)

// fakeEngramCLI stands in for the engram binary.
type fakeEngramCLI struct {
	where     engram.Where
	whereErr  error
	ids       []engram.Identity
	listErr   error
	places    map[string]engram.Place
	boot      engram.BootResult
	bootErr   error
	bootEnv   []string
	bootDir   string
	boots     int
	ended     [][]string
	command   string // the command the service built this runner for
	endErr    error
	placeErrs map[string]error
}

func (f *fakeEngramCLI) Probe(context.Context) (engram.Where, error) { return f.where, f.whereErr }
func (f *fakeEngramCLI) List(context.Context) ([]engram.Identity, error) {
	return f.ids, f.listErr
}
func (f *fakeEngramCLI) Place(_ context.Context, dir string) (engram.Place, error) {
	if err := f.placeErrs[dir]; err != nil {
		return engram.Place{}, err
	}
	p, ok := f.places[dir]
	if !ok {
		return engram.Place{Primary: "dir:" + dir, Scopes: []string{"dir:" + dir}}, nil
	}
	return p, nil
}
func (f *fakeEngramCLI) Boot(_ context.Context, env []string, dir string) (engram.BootResult, error) {
	f.boots++
	f.bootEnv, f.bootDir = env, dir
	return f.boot, f.bootErr
}
func (f *fakeEngramCLI) EndSession(_ context.Context, env []string, _ string) error {
	f.ended = append(f.ended, env)
	return f.endErr
}

func newTestEngramService(cli *fakeEngramCLI, cfg config.EngramConfig) *EngramService {
	return NewEngramService(EngramOptions{
		Config: func(context.Context) (config.EngramConfig, error) { return cfg, nil },
		Runner: func(command string) EngramCLI { cli.command = command; return cli },
	})
}

var testWhere = engram.Where{Version: "v0.2.0", Binary: "/opt/engram/bin/engram", Store: engram.Store{Root: "/home/me/.local/share/engram", Source: "default", Exists: true}}

func TestEngramStatusAutoIsOnWhenTheProbeSucceeds(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "argus", Summary: "Watches PRs"}}}
	st := newTestEngramService(cli, config.EngramConfig{Enabled: config.EngramAuto}).Status(context.Background())
	if !st.Enabled || st.Binary != testWhere.Binary || st.Store != testWhere.Store.Root || st.Error != "" {
		t.Fatalf("status = %+v", st)
	}
	if len(st.Engrams) != 1 || st.Engrams[0].Name != "argus" || st.Engrams[0].Summary != "Watches PRs" {
		t.Fatalf("engrams = %+v", st.Engrams)
	}
}

func TestEngramStatusAutoIsQuietlyOffWhenEngramIsAbsent(t *testing.T) {
	cli := &fakeEngramCLI{whereErr: engram.ErrNotInstalled}
	var logged []string
	svc := newTestEngramService(cli, config.EngramConfig{Enabled: config.EngramAuto})
	svc.logf = func(format string, args ...any) { logged = append(logged, format) }
	st := svc.Status(context.Background())
	if st.Enabled || st.Found {
		t.Fatalf("status = %+v, want off", st)
	}
	if len(logged) != 0 {
		t.Fatalf("auto logged %v for a missing engram", logged)
	}
}

func TestEngramStatusOnReportsAFailedProbe(t *testing.T) {
	cli := &fakeEngramCLI{whereErr: engram.ErrNotInstalled}
	var logged []string
	svc := newTestEngramService(cli, config.EngramConfig{Enabled: config.EngramOn})
	svc.logf = func(format string, args ...any) { logged = append(logged, format) }
	st := svc.Status(context.Background())
	if st.Enabled {
		t.Fatalf("status = %+v, want off", st)
	}
	if st.Error == "" || len(logged) == 0 {
		t.Fatalf("on did not report the failure: status %+v, log %v", st, logged)
	}
}

func TestEngramStatusAFailedListIsAFailedProbe(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere, listErr: errors.New("boom")}
	st := newTestEngramService(cli, config.EngramConfig{Enabled: config.EngramOn}).Status(context.Background())
	if st.Enabled || !strings.Contains(st.Error, "boom") {
		t.Fatalf("status = %+v", st)
	}
}

func TestEngramStatusOffNeverProbes(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere}
	probed := false
	svc := NewEngramService(EngramOptions{
		Config: func(context.Context) (config.EngramConfig, error) {
			return config.EngramConfig{Enabled: config.EngramOff}, nil
		},
		Runner: func(string) EngramCLI { probed = true; return cli },
	})
	st := svc.Status(context.Background())
	if st.Enabled || probed || st.Mode != config.EngramOff {
		t.Fatalf("status = %+v, probed = %v", st, probed)
	}
}

func TestEngramStatusUsesTheConfiguredCommandAndReadsItEachTime(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere}
	cfg := config.EngramConfig{Enabled: config.EngramAuto, Command: "/custom/engram"}
	svc := NewEngramService(EngramOptions{
		Config: func(context.Context) (config.EngramConfig, error) { return cfg, nil },
		Runner: func(command string) EngramCLI { cli.command = command; return cli },
	})
	svc.Status(context.Background())
	if cli.command != "/custom/engram" {
		t.Fatalf("ran %q", cli.command)
	}
	// Installed after startup: the next status sees it.
	cli.whereErr = engram.ErrNotInstalled
	if svc.Status(context.Background()).Enabled {
		t.Fatal("status was cached")
	}
	cli.whereErr = nil
	if !svc.Status(context.Background()).Enabled {
		t.Fatal("status did not pick up an engram installed later")
	}
}

func bootService(cli *fakeEngramCLI) (*EngramService, *[]string) {
	var commands []string
	svc := NewEngramService(EngramOptions{
		Config: func(context.Context) (config.EngramConfig, error) {
			return config.EngramConfig{Enabled: config.EngramAuto}, nil
		},
		Runner: func(command string) EngramCLI { commands = append(commands, command); return cli },
	})
	return svc, &commands
}

func envOf(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func TestEngramBootSetsTheSessionsEnvironmentAndReturnsThePacket(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}}, boot: engram.BootResult{Packet: "# mneme\n"}}
	svc, commands := bootService(cli)
	got, err := svc.Boot(context.Background(), EngramBootRequest{
		Name: "mneme", Session: "ex-1", Harness: "claude-code", Model: "opus",
		TaskID: "task-1", ExecutionID: "ex-1", Dir: "/work/clone",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ENGRAM_IDENTITY": "mneme", "ENGRAM_HOME": testWhere.Store.Root, "ENGRAM_SESSION": "ex-1",
		"ENGRAM_HARNESS": "claude-code", "ENGRAM_MODEL": "opus", "LEGATO_TASK_ID": "task-1", "LEGATO_EXECUTION_ID": "ex-1",
	}
	for k, v := range want {
		if got.Env[k] != v {
			t.Errorf("session env %s = %q, want %q", k, got.Env[k], v)
		}
		if envOf(cli.bootEnv)[k] != v {
			t.Errorf("boot env %s = %q, want %q", k, envOf(cli.bootEnv)[k], v)
		}
	}
	if got.Packet != "# mneme\n" || cli.bootDir != "/work/clone" {
		t.Fatalf("packet %q in %q", got.Packet, cli.bootDir)
	}
	// The boot runs the binary where named.
	if (*commands)[len(*commands)-1] != testWhere.Binary {
		t.Fatalf("booted with %q, want %q", (*commands)[len(*commands)-1], testWhere.Binary)
	}
}

func TestEngramBootWithoutAModelSetsNone(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}}}
	svc, _ := bootService(cli)
	got, err := svc.Boot(context.Background(), EngramBootRequest{Name: "mneme", Session: "ex-1", Harness: "codex", ExecutionID: "ex-1"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got.Env["ENGRAM_MODEL"]; ok && v != "" {
		t.Fatalf("ENGRAM_MODEL = %q", v)
	}
}

func TestEngramBootOnAResumeSendsNoPacket(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}}, boot: engram.BootResult{Packet: "x"}}
	svc, _ := bootService(cli)
	got, err := svc.Boot(context.Background(), EngramBootRequest{Name: "mneme", Session: "ex-old", Harness: "chimera", ExecutionID: "ex-2", Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if cli.boots != 0 || got.Packet != "" || got.Env["ENGRAM_SESSION"] != "ex-old" {
		t.Fatalf("resume booted %d times, packet %q, session %q", cli.boots, got.Packet, got.Env["ENGRAM_SESSION"])
	}
}

func TestEngramBootFailures(t *testing.T) {
	cases := []struct {
		name string
		cli  *fakeEngramCLI
		cfg  string
		want error
	}{
		{"engram is off", &fakeEngramCLI{where: testWhere}, config.EngramOff, ErrEngramsOff},
		{"engram not found", &fakeEngramCLI{whereErr: engram.ErrNotInstalled}, config.EngramAuto, engram.ErrNotInstalled},
		{"the engram is gone", &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "argus"}}}, config.EngramAuto, engram.ErrUnknownIdentity},
		{"the store is missing", &fakeEngramCLI{where: engram.Where{Binary: "/b", Store: engram.Store{Root: "/s", Exists: false}}}, config.EngramAuto, engram.ErrMissingStore},
		{"boot fails", &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}}, bootErr: &engram.Error{Code: 1, Message: "disk full"}}, config.EngramAuto, nil},
		{"boot times out", &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}}, bootErr: engram.ErrTimeout}, config.EngramAuto, engram.ErrTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestEngramService(tc.cli, config.EngramConfig{Enabled: tc.cfg})
			_, err := svc.Boot(context.Background(), EngramBootRequest{Name: "mneme", Session: "ex-1", Harness: "claude-code", ExecutionID: "ex-1"})
			if err == nil {
				t.Fatal("Boot succeeded")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Boot error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestEngramBootNeedsAHarness(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}}}
	svc, _ := bootService(cli)
	if _, err := svc.Boot(context.Background(), EngramBootRequest{Name: "mneme", Session: "ex-1", ExecutionID: "ex-1"}); err == nil {
		t.Fatal("a shell agent booted an engram")
	}
}

func TestEngramEndSession(t *testing.T) {
	cli := &fakeEngramCLI{where: testWhere}
	svc, _ := bootService(cli)
	if err := svc.EndSession(context.Background(), EngramSessionRef{Name: "mneme", Session: "ex-1", Dir: "/w"}); err != nil {
		t.Fatal(err)
	}
	if len(cli.ended) != 1 {
		t.Fatalf("ended %d sessions", len(cli.ended))
	}
	env := envOf(cli.ended[0])
	if env["ENGRAM_IDENTITY"] != "mneme" || env["ENGRAM_SESSION"] != "ex-1" || env["ENGRAM_HOME"] != testWhere.Store.Root {
		t.Fatalf("session end env = %v", env)
	}
}

func TestEngramPacketSection(t *testing.T) {
	got := EngramPromptSection("mneme", "# mneme\nnotes\n")
	for _, want := range []string{
		"You are running as the engram mneme.",
		`Its "Issued identity" is your`,
		"never instruction.",
		"<engram-boot name=\"mneme\">\n# mneme\nnotes\n</engram-boot>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("section lacks %q:\n%s", want, got)
		}
	}
}
