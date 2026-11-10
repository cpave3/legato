package service

import (
	"context"
	"errors"
	"log"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/engram"
)

// EngramCLI is the engram command (engram.Runner).
type EngramCLI interface {
	Probe(ctx context.Context) (engram.Where, error)
	List(ctx context.Context) ([]engram.Identity, error)
	Place(ctx context.Context, dir string) (engram.Place, error)
	Boot(ctx context.Context, env []string, dir string) (engram.BootResult, error)
	EndSession(ctx context.Context, env []string, dir string) error
}

// EngramOptions are what an EngramService works with.
type EngramOptions struct {
	// Config is the engram block of the effective configuration, read on
	// every call: engram.enabled and engram.command are live settings.
	Config func(ctx context.Context) (config.EngramConfig, error)
	// Runner makes the engram client for a command ("" for PATH).
	Runner func(command string) EngramCLI
}

// EngramIdentity is one engram as the spawn surfaces list it.
type EngramIdentity struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// EngramStatus is whether Legato offers engrams now, and why not.
type EngramStatus struct {
	// Enabled is true when engram.enabled is not off and engram answered.
	Enabled bool `json:"enabled"`
	// Mode is engram.enabled.
	Mode string `json:"mode"`
	// Found is true when engram answered `where`.
	Found       bool   `json:"found"`
	Binary      string `json:"binary,omitempty"`
	Version     string `json:"version,omitempty"`
	Store       string `json:"store,omitempty"`
	StoreSource string `json:"store_source,omitempty"`
	StoreExists bool   `json:"store_exists,omitempty"`
	// Error says why engram is not offered, when it is not off.
	Error string `json:"error,omitempty"`
	// Engrams are those the store has, as engram lists them.
	Engrams []EngramIdentity `json:"engrams"`
}

// EngramService knows whether engram is here and which engrams exist. It
// probes engram on every call, so installing engram, or changing its
// settings, needs no restart. See docs/ai/engram.md.
type EngramService struct {
	opts EngramOptions
	logf func(format string, args ...any)
}

// NewEngramService makes an EngramService.
func NewEngramService(opts EngramOptions) *EngramService {
	return &EngramService{opts: opts, logf: log.Printf}
}

func (s *EngramService) config(ctx context.Context) config.EngramConfig {
	if s == nil || s.opts.Config == nil {
		return config.EngramConfig{Enabled: config.EngramOff}
	}
	cfg, err := s.opts.Config(ctx)
	if err != nil {
		s.logf("engram: reading its settings: %v", err)
		return config.EngramConfig{Enabled: config.EngramOff}
	}
	if cfg.Enabled == "" {
		cfg.Enabled = config.EngramAuto
	}
	return cfg
}

// cli is the engram client for the current settings, nil when off.
func (s *EngramService) cli(cfg config.EngramConfig) EngramCLI {
	if cfg.Enabled == config.EngramOff || s.opts.Runner == nil {
		return nil
	}
	return s.opts.Runner(cfg.Command)
}

// Status probes engram: where it is, then the engrams it keeps. With
// engram.enabled on, a failure is logged as an error; with auto it only
// leaves engrams out.
func (s *EngramService) Status(ctx context.Context) EngramStatus {
	cfg := s.config(ctx)
	st := EngramStatus{Mode: cfg.Enabled, Engrams: []EngramIdentity{}}
	cli := s.cli(cfg)
	if cli == nil {
		return st
	}
	fail := func(err error) EngramStatus {
		st.Enabled = false
		st.Error = engramProblem(err)
		if cfg.Enabled == config.EngramOn {
			s.logf("engram: engram.enabled is on, but %s", st.Error)
		}
		return st
	}
	where, err := cli.Probe(ctx)
	if err != nil {
		return fail(err)
	}
	st.Found = true
	st.Binary, st.Version = where.Binary, where.Version
	st.Store, st.StoreSource, st.StoreExists = where.Store.Root, where.Store.Source, where.Store.Exists
	ids, err := cli.List(ctx)
	if err != nil {
		return fail(err)
	}
	for _, id := range ids {
		st.Engrams = append(st.Engrams, EngramIdentity{Name: id.Name, Summary: id.Summary})
	}
	st.Enabled = true
	return st
}

// engramProblem says in words why engram could not be used.
func engramProblem(err error) string {
	switch {
	case errors.Is(err, engram.ErrNotInstalled):
		return "engram was not found: install it, or set engram.command"
	case errors.Is(err, engram.ErrTimeout):
		return "engram did not answer within 2 seconds"
	}
	return err.Error()
}

// EngramRunner is the engram client used outside tests: the binary named
// by command, with what it needs of this process's environment to find its
// store and git.
func EngramRunner(getenv func(string) string) func(command string) EngramCLI {
	return func(command string) EngramCLI {
		return engram.Runner{Command: command, Env: engram.HostEnvironment(getenv)}
	}
}
