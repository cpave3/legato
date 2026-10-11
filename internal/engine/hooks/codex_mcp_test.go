package hooks_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// shellArgs returns the arguments a launch command becomes once the pane's
// shell has parsed it, which is what the agent tool is really started with.
func shellArgs(t *testing.T, command string) []string {
	t.Helper()
	out, err := exec.Command("sh", "-c", "set -- "+command+`; for arg in "$@"; do printf '%s\0' "$arg"; done`).Output()
	if err != nil {
		t.Fatalf("the shell cannot parse %q: %v", command, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

// configOverrides returns the value of each `-c key=value` argument by key.
func configOverrides(args []string) map[string]string {
	overrides := map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" {
			if key, value, ok := strings.Cut(args[i+1], "="); ok {
				overrides[key] = value
			}
		}
	}
	return overrides
}

func TestCodexAdapterGivesASessionLegatosMCPServerAtLaunch(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/opt/my tools/legato")
	adapter.SetMCPTools(true)
	adapter.SetLaunchArgs([]string{"--search"})

	args := shellArgs(t, adapter.LaunchCommand(nil, "", "", ""))

	if args[0] != "codex" {
		t.Fatalf("command = %q", args[0])
	}
	overrides := configOverrides(args)
	// Each value is TOML: Codex reads these as it reads its own config file,
	// and nothing is written to that file.
	if overrides["mcp_servers.legato.command"] != `"/opt/my tools/legato"` {
		t.Fatalf("server command = %q", overrides["mcp_servers.legato.command"])
	}
	if overrides["mcp_servers.legato.args"] != `["mcp","serve"]` {
		t.Fatalf("server args = %q", overrides["mcp_servers.legato.args"])
	}
	// Codex starts an MCP server with a short list of variables. The server
	// finds its session, and the same Legato, through these.
	forwarded := overrides["mcp_servers.legato.env_vars"]
	for _, name := range []string{"LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_CONFIG", "XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		if !strings.Contains(forwarded, `"`+name+`"`) {
			t.Errorf("env_vars %s does not forward %s", forwarded, name)
		}
	}
	if args[len(args)-1] != "--search" {
		t.Fatalf("the person's launch args must come last so theirs win: %q", args)
	}
}

func TestCodexAdapterGivesTheServerToTheNativeLauncherToo(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.SetNativeBridge(true)
	adapter.SetMCPTools(true)

	args := shellArgs(t, adapter.LaunchCommand(map[string]string{}, "", "", ""))

	if strings.Join(args[:4], " ") != "/usr/bin/legato agent codex --" {
		t.Fatalf("launcher = %q", args)
	}
	// The launcher hands every -c to the session's app server, which is the
	// process that starts MCP servers.
	if configOverrides(args)["mcp_servers.legato.command"] != `"/usr/bin/legato"` {
		t.Fatalf("launcher arguments carry no server: %q", args)
	}
}

func TestCodexAdapterWithoutToolsDefinesNoServer(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")

	if command := adapter.LaunchCommand(nil, "", "", ""); strings.Contains(command, "mcp_servers") {
		t.Fatalf("command = %q", command)
	}
}

// Codex does not pass an MCP server's instructions to its model, so the
// adapter offers the same guidance for the launch prompt.
func TestCodexAdapterOffersToolsGuidanceOnlyWithTheTools(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.SetToolsPrompt("## Legato tools\n\nUse show_card without being asked.")

	if got := adapter.ToolsPrompt(); got != "" {
		t.Fatalf("guidance for tools the session does not have: %q", got)
	}

	adapter.SetMCPTools(true)

	if got := adapter.ToolsPrompt(); got != "## Legato tools\n\nUse show_card without being asked." {
		t.Fatalf("guidance = %q", got)
	}
}

// Claude Code reads the server's own instructions, so it needs no second copy
// in its launch prompt.
func TestClaudeCodeAdapterAddsNoToolsGuidanceToItsLaunchPrompt(t *testing.T) {
	var adapter any = hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	if _, has := adapter.(interface{ ToolsPrompt() string }); has {
		t.Fatal("the Claude Code adapter repeats guidance its sessions already get from the server")
	}
}
