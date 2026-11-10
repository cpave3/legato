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
