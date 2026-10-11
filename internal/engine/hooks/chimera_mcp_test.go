package hooks_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// mcpConfigArg returns the value of --mcp-config in a launch command, as the
// pane's shell hands it to Chimera.
func mcpConfigArg(t *testing.T, command string) (string, bool) {
	t.Helper()
	args := shellArgs(t, command)
	for i, arg := range args {
		if arg == "--mcp-config" && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func TestChimeraAdapterGivesASessionLegatosMCPServerAtLaunch(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/opt/my tools/legato")
	adapter.SetMCPTools(true, []string{"show_card", "show_artifact"})
	adapter.SetMCPConfigProbe(func() bool { return true })
	adapter.SetLaunchArgs([]string{"--sandbox"})

	command := adapter.LaunchCommand(map[string]string{}, "", "", "")

	value, ok := mcpConfigArg(t, command)
	if !ok {
		t.Fatalf("no --mcp-config in %q", command)
	}
	var document struct {
		MCPServers map[string]struct {
			Transport    string            `json:"transport"`
			Command      string            `json:"command"`
			Args         []string          `json:"args"`
			Env          map[string]string `json:"env"`
			TrustedTools []string          `json:"trustedTools"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(value), &document); err != nil {
		t.Fatalf("--mcp-config is not JSON: %v\n%s", err, value)
	}
	server, found := document.MCPServers["legato"]
	if !found || len(document.MCPServers) != 1 {
		t.Fatalf("servers = %#v, want only legato", document.MCPServers)
	}
	if server.Transport != "stdio" || server.Command != "/opt/my tools/legato" || strings.Join(server.Args, " ") != "mcp serve" {
		t.Fatalf("legato server = %#v", server)
	}
	// An env entry is what makes Chimera pass the session's environment to
	// the server; it must name a variable every session has, because Chimera
	// refuses to start a server whose reference is unset.
	if len(server.Env) != 1 || server.Env["LEGATO_TASK_ID"] != "env:LEGATO_TASK_ID" {
		t.Fatalf("env = %#v", server.Env)
	}
	// The person chose not to be asked before an agent uses Legato's tools.
	if strings.Join(server.TrustedTools, ",") != "show_card,show_artifact" {
		t.Fatalf("trusted tools = %v", server.TrustedTools)
	}
	if args := shellArgs(t, command); args[len(args)-1] != "--sandbox" {
		t.Fatalf("the person's launch args must come last: %q", args)
	}
}

// A Chimera from before the flag existed would refuse to start at all.
func TestChimeraAdapterLeavesAnOlderChimeraAlone(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	adapter.SetMCPTools(true, []string{"show_card"})
	probes := 0
	adapter.SetMCPConfigProbe(func() bool { probes++; return false })

	for i := 0; i < 3; i++ {
		if command := adapter.LaunchCommand(map[string]string{}, "", "", ""); strings.Contains(command, "--mcp-config") {
			t.Fatalf("command = %q", command)
		}
	}
	// Asking Chimera what it supports starts a process; once is enough.
	if probes != 1 {
		t.Fatalf("Chimera was asked %d times, want once", probes)
	}
}

func TestChimeraAdapterWithoutToolsNeitherAsksNorAddsTheFlag(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	adapter.SetMCPConfigProbe(func() bool {
		t.Fatal("Chimera was asked about a flag nobody wants")
		return true
	})

	if command := adapter.LaunchCommand(map[string]string{}, "", "", ""); strings.Contains(command, "--mcp-config") {
		t.Fatalf("command = %q", command)
	}
}

func TestChimeraAdapterKeepsThePersonsOwnMCPConfigFlag(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	adapter.SetMCPTools(true, []string{"show_card"})
	adapter.SetMCPConfigProbe(func() bool { return true })
	adapter.SetLaunchArgs([]string{"--mcp-config", "@/home/me/servers.json"})

	value, _ := mcpConfigArg(t, adapter.LaunchCommand(map[string]string{}, "", "", ""))

	// Chimera takes the flag once; two would silently drop one of them.
	if value != "@/home/me/servers.json" {
		t.Fatalf("--mcp-config = %q, want the person's own", value)
	}
}

func TestChimeraAdapterAddsTheServerWhenResumingASessionToo(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	adapter.SetMCPTools(true, []string{"show_card"})
	adapter.SetMCPConfigProbe(func() bool { return true })

	command := adapter.LaunchCommand(map[string]string{"LEGATO_CHIMERA_SESSION_EXISTS": "resume", "LEGATO_CHIMERA_SESSION_ID": "task1"}, "", "", "")

	if _, ok := mcpConfigArg(t, command); !ok || !strings.Contains(command, "--session task1") {
		t.Fatalf("command = %q", command)
	}
}

// Chimera does not pass an MCP server's instructions to its model either.
func TestChimeraAdapterOffersToolsGuidanceOnlyWhenItAddsTheServer(t *testing.T) {
	const guidance = "## Legato tools\n\nUse show_card without being asked."
	newAdapter := func(supported bool, launchArgs ...string) *hooks.ChimeraAdapter {
		adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
		adapter.SetToolsPrompt(guidance)
		adapter.SetMCPConfigProbe(func() bool { return supported })
		adapter.SetLaunchArgs(launchArgs)
		return adapter
	}

	withoutTools := newAdapter(true)
	if got := withoutTools.ToolsPrompt(); got != "" {
		t.Fatalf("guidance with the tools off: %q", got)
	}

	withTools := newAdapter(true)
	withTools.SetMCPTools(true, []string{"show_card"})
	if got := withTools.ToolsPrompt(); got != guidance {
		t.Fatalf("guidance = %q", got)
	}

	// An older Chimera gets no server, and so nothing to be told about.
	older := newAdapter(false)
	older.SetMCPTools(true, []string{"show_card"})
	if got := older.ToolsPrompt(); got != "" {
		t.Fatalf("guidance for an older Chimera that gets no tools: %q", got)
	}

	// The person's own --mcp-config replaces Legato's, so the tools are absent.
	overridden := newAdapter(true, "--mcp-config", "@/home/me/servers.json")
	overridden.SetMCPTools(true, []string{"show_card"})
	if got := overridden.ToolsPrompt(); got != "" {
		t.Fatalf("guidance when the person's own flag replaced the server: %q", got)
	}
}
