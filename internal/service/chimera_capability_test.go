package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestChimeraMCPLaunchRefusesAHostWithoutModsBeforeStartingTerminal(t *testing.T) {
	for _, version := range []string{`{"version":"old","release":true}`, `{"modApi":[2]}`, `not-json`} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "chimera"), []byte("#!/bin/sh\nprintf '%s' '"+version+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			svc, s, mt := newNativeLaunchService(t, t.TempDir())
			a := hooks.NewChimeraAdapter("legato")
			a.SetModDir(t.TempDir())
			a.SetMCPTools(true, []string{"task_context"})
			svc.(*agentService).adapter = a
			createTask(t, s, "old-chimera")
			err := svc.SpawnAgent(context.Background(), "old-chimera", 0, 0)
			if err == nil || !strings.Contains(err.Error(), "upgrade Chimera") || !strings.Contains(err.Error(), "native_bridge: false") {
				t.Fatalf("want actionable upgrade/opt-out error, got %v", err)
			}
			if mt.sessionAlive("legato-old-chimera") || len(mt.sentLinesFor("legato-old-chimera")) != 0 {
				t.Fatal("unsupported host started without its MCP tools")
			}
		})
	}
}

func TestChimeraMCPLaunchChecksCurrentCapabilityAtEverySpawn(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "chimera")
	writeVersion := func(version string) {
		t.Helper()
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' '"+version+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeVersion(`{"version":"new","modApi":[1]}`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	a := hooks.NewChimeraAdapter("legato")
	a.SetModDir(t.TempDir())
	a.SetMCPTools(true, []string{"task_context"})
	svc.(*agentService).adapter = a
	createTask(t, s, "supported")
	if err := svc.SpawnAgent(context.Background(), "supported", 0, 0); err != nil {
		t.Fatal(err)
	}
	lines := mt.sentLinesFor("legato-supported")
	if len(lines) != 1 || strings.Contains(lines[0], "--mcp-config") {
		t.Fatalf("mod-capable launch lost bundled transport: %v", lines)
	}
	writeVersion(`{"version":"old"}`)
	createTask(t, s, "downgraded")
	if err := svc.SpawnAgent(context.Background(), "downgraded", 0, 0); err == nil {
		t.Fatal("cached old capability across binary replacement")
	}
}

func TestChimeraLegacyMCPLaunchDoesNotRequireModCapability(t *testing.T) {
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	a := hooks.NewChimeraAdapter("legato")
	a.SetMCPTools(true, []string{"task_context"})
	a.SetMCPConfigProbe(func() bool { return true })
	svc.(*agentService).adapter = a
	createTask(t, s, "legacy-tools")
	if err := svc.SpawnAgent(context.Background(), "legacy-tools", 0, 0); err != nil {
		t.Fatal(err)
	}
	lines := mt.sentLinesFor("legato-legacy-tools")
	if len(lines) != 1 || !strings.Contains(lines[0], "--mcp-config") || !strings.Contains(lines[0], "task_context") {
		t.Fatalf("legacy launch lost its MCP server: %v", lines)
	}
}

func TestChimeraMCPLaunchRefusesDisabledModsInEveryArgumentSource(t *testing.T) {
	for _, source := range []string{"base", "tier", "spawn"} {
		t.Run(source, func(t *testing.T) {
			svc, s, mt := newNativeLaunchService(t, t.TempDir())
			a := hooks.NewChimeraAdapter("legato")
			a.SetModDir(t.TempDir())
			a.SetMCPTools(true, []string{"task_context"})
			svc.(*agentService).adapter = a
			opt := AgentSpawnOptions{Ephemeral: true}
			switch source {
			case "base":
				a.SetLaunchArgs([]string{"--no-mods"})
			case "tier":
				a.SetTiers(map[string][]string{"off": {"--no-mods"}})
				opt.Tier = "off"
			case "spawn":
				opt.LaunchArgs = []string{"--no-mods"}
			}
			createTask(t, s, "disabled-mods")
			if err := svc.SpawnAgent(context.Background(), "disabled-mods", 0, 0, opt); err == nil || !strings.Contains(err.Error(), "remove --no-mods") {
				t.Fatalf("want actionable disabled-mods error, got %v", err)
			}
			if mt.sessionAlive("legato-disabled-mods") {
				t.Fatal("started with bundled tools disabled")
			}
		})
	}
}
