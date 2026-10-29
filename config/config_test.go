package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", path)
	return path
}

func TestGitHubAuthDefaultsToAutoAndValidatesModes(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHub.Auth != "auto" {
		t.Fatalf("auth = %q, want auto", cfg.GitHub.Auth)
	}

	writeTestConfig(t, "github:\n  auth: mutate\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "github.auth") {
		t.Fatalf("error = %v", err)
	}
}

func TestWorkspaceDefaultsLoadAndMatchConfiguredNamesCaseInsensitively(t *testing.T) {
	writeTestConfig(t, `
workspaces:
  - name: Work
    color: "#4A9EEF"
  - name: Personal
    color: "#7BC47F"
default_workspace: personal
default_import_workspace: WORK
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultWorkspace != "personal" {
		t.Errorf("DefaultWorkspace = %q, want personal", cfg.DefaultWorkspace)
	}
	if cfg.DefaultImportWorkspace != "WORK" {
		t.Errorf("DefaultImportWorkspace = %q, want WORK", cfg.DefaultImportWorkspace)
	}
}

func TestWorkspaceDefaultsAreOptional(t *testing.T) {
	writeTestConfig(t, "workspaces:\n  - name: Work\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultWorkspace != "" || cfg.DefaultImportWorkspace != "" {
		t.Fatalf("workspace defaults = %q, %q; want empty", cfg.DefaultWorkspace, cfg.DefaultImportWorkspace)
	}
}

func TestWorkspaceDefaultsRejectUnknownAndAmbiguousNames(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "unknown default workspace",
			config:  "workspaces:\n  - name: Work\ndefault_workspace: Personal\n",
			wantErr: `default_workspace "Personal" does not match a configured workspace`,
		},
		{
			name:    "unknown import workspace",
			config:  "workspaces:\n  - name: Work\ndefault_import_workspace: Personal\n",
			wantErr: `default_import_workspace "Personal" does not match a configured workspace`,
		},
		{
			name:    "duplicate names",
			config:  "workspaces:\n  - name: Work\n  - name: work\n",
			wantErr: `duplicate workspace name "work"`,
		},
		{
			name:    "blank name",
			config:  "workspaces:\n  - name: \"  \"\n",
			wantErr: "workspace name must not be blank",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeTestConfig(t, tt.config)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestGitHubTokenExpandsFromEnvironment(t *testing.T) {
	t.Setenv("LEGATO_GITHUB_TOKEN", "github-secret")
	writeTestConfig(t, `
github:
  token: ${LEGATO_GITHUB_TOKEN}
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHub.Token != "github-secret" {
		t.Fatalf("GitHub config = %+v", cfg.GitHub)
	}
}

func TestGroupsDefaultsParsed(t *testing.T) {
	writeTestConfig(t, `
groups:
  defaults:
    - backend
    - urgent
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups.Defaults) != 2 || cfg.Groups.Defaults[0] != "backend" || cfg.Groups.Defaults[1] != "urgent" {
		t.Errorf("Groups.Defaults = %v, want [backend urgent]", cfg.Groups.Defaults)
	}
}

func TestGroupsDefaultsAbsentIsEmpty(t *testing.T) {
	writeTestConfig(t, `theme: default`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups.Defaults) != 0 {
		t.Errorf("Groups.Defaults = %v, want empty or nil", cfg.Groups.Defaults)
	}
}

func TestVoiceConfigParsed(t *testing.T) {
	writeTestConfig(t, `
voice:
  enabled: true
  whisper_url: "http://192.168.1.50:8000/v1/audio/transcriptions"
  autosend: true
  mic_device: "hw:1,0"
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Voice.Enabled {
		t.Error("Voice.Enabled = false, want true")
	}
	if cfg.Voice.WhisperURL != "http://192.168.1.50:8000/v1/audio/transcriptions" {
		t.Errorf("Voice.WhisperURL = %q, want the configured URL", cfg.Voice.WhisperURL)
	}
	if !cfg.Voice.AutoSend {
		t.Error("Voice.AutoSend = false, want true")
	}
	if cfg.Voice.MicDevice != "hw:1,0" {
		t.Errorf("Voice.MicDevice = %q, want %q", cfg.Voice.MicDevice, "hw:1,0")
	}
}

func TestVoiceConfigAbsentDefaultsToDisabled(t *testing.T) {
	writeTestConfig(t, `theme: default`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Voice.Enabled {
		t.Error("Voice.Enabled should default to false")
	}
	if cfg.Voice.WhisperURL != "" {
		t.Errorf("Voice.WhisperURL = %q, want empty", cfg.Voice.WhisperURL)
	}
	if cfg.Voice.AutoSend {
		t.Error("Voice.AutoSend should default to false")
	}
	if cfg.Voice.MicDevice != "" {
		t.Errorf("Voice.MicDevice = %q, want empty", cfg.Voice.MicDevice)
	}
}

func TestJiraStatusSyncDefaultsToEnabled(t *testing.T) {
	writeTestConfig(t, `theme: default`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Jira.StatusSyncEnabled {
		t.Error("Jira.StatusSyncEnabled = false, want default true")
	}
}

func TestJiraStatusSyncCanBeDisabled(t *testing.T) {
	writeTestConfig(t, `
jira:
  status_sync_enabled: false
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira.StatusSyncEnabled {
		t.Error("Jira.StatusSyncEnabled = true, want false")
	}
}

func TestYggdrasilWorktreeConfigParsed(t *testing.T) {
	writeTestConfig(t, `
worktrees:
  yggdrasil:
    enabled: true
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Worktrees.Yggdrasil.Enabled {
		t.Error("Worktrees.Yggdrasil.Enabled = false, want true")
	}
}

func TestYggdrasilWorktreeConfigAbsentDefaultsToDisabled(t *testing.T) {
	writeTestConfig(t, `theme: default`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktrees.Yggdrasil.Enabled {
		t.Error("Worktrees.Yggdrasil.Enabled should default to false")
	}
}

func TestResolveConfigPathUsesLEGATO_CONFIG(t *testing.T) {
	t.Setenv("LEGATO_CONFIG", "/tmp/my-legato.yaml")
	t.Setenv("XDG_CONFIG_HOME", "/should/not/use")

	got := ResolveConfigPath()
	if got != "/tmp/my-legato.yaml" {
		t.Errorf("got %q, want %q", got, "/tmp/my-legato.yaml")
	}
}

func TestResolveConfigPathUsesXDG(t *testing.T) {
	t.Setenv("LEGATO_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/home/user/.myconfig")

	got := ResolveConfigPath()
	want := "/home/user/.myconfig/legato/config.yaml"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveConfigPathUsesDefault(t *testing.T) {
	t.Setenv("LEGATO_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	got := ResolveConfigPath()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "legato", "config.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadParsesYAMLWithEnvExpansion(t *testing.T) {
	t.Setenv("LEGATO_JIRA_TOKEN", "secret123")
	writeTestConfig(t, `
jira:
  base_url: "https://example.atlassian.net"
  email: "user@example.com"
  api_token: "${LEGATO_JIRA_TOKEN}"
  project_keys:
    - "REX"
  sync_interval_seconds: 120
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira.BaseURL != "https://example.atlassian.net" {
		t.Errorf("BaseURL = %q, want %q", cfg.Jira.BaseURL, "https://example.atlassian.net")
	}
	if cfg.Jira.APIToken != "secret123" {
		t.Errorf("APIToken = %q, want %q", cfg.Jira.APIToken, "secret123")
	}
	if cfg.Jira.SyncIntervalSeconds != 120 {
		t.Errorf("SyncIntervalSeconds = %d, want 120", cfg.Jira.SyncIntervalSeconds)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	writeTestConfig(t, `
jira:
  base_url: "https://example.atlassian.net"
  email: "user@example.com"
  api_token: "token"
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira.SyncIntervalSeconds != 60 {
		t.Errorf("SyncIntervalSeconds = %d, want default 60", cfg.Jira.SyncIntervalSeconds)
	}
	if cfg.Theme != "default" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "default")
	}
	if !cfg.Keybindings.VimMode {
		t.Error("VimMode should default to true")
	}
	if len(cfg.Board.Columns) != 5 {
		t.Errorf("Board.Columns count = %d, want 5 defaults", len(cfg.Board.Columns))
	}
}

func TestLoadReturnsDefaultsWhenNoConfigFile(t *testing.T) {
	t.Setenv("LEGATO_CONFIG", "/nonexistent/path/config.yaml")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Jira.SyncIntervalSeconds != 60 {
		t.Errorf("SyncIntervalSeconds = %d, want default 60", cfg.Jira.SyncIntervalSeconds)
	}
}

func TestAgentsPeerAwarenessDefaultsOnAndCanBeDisabled(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("LEGATO_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Agents.PeerAwareness {
			t.Fatal("peer awareness should default on")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("agents:\n  peer_awareness: false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("LEGATO_CONFIG", path)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Agents.PeerAwareness {
			t.Fatal("peer awareness should honor explicit false")
		}
	})
}

func TestAgentsEscapeKeyDefault(t *testing.T) {
	writeTestConfig(t, `
jira:
  base_url: "https://example.atlassian.net"
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.EscapeKey != "ctrl+]" {
		t.Errorf("EscapeKey = %q, want %q", cfg.Agents.EscapeKey, "ctrl+]")
	}
}

func TestAgentsEscapeKeyCustom(t *testing.T) {
	writeTestConfig(t, `
agents:
  escape_key: "ctrl+\\"
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.EscapeKey != `ctrl+\` {
		t.Errorf("EscapeKey = %q, want %q", cfg.Agents.EscapeKey, `ctrl+\`)
	}
}

func TestResolveEditorPrecedence(t *testing.T) {
	t.Run("config editor takes priority", func(t *testing.T) {
		t.Setenv("VISUAL", "code --wait")
		t.Setenv("EDITOR", "nano")
		cfg := &Config{Editor: "nvim"}
		got := ResolveEditor(cfg)
		if got != "nvim" {
			t.Errorf("got %q, want %q", got, "nvim")
		}
	})

	t.Run("VISUAL when no config editor", func(t *testing.T) {
		t.Setenv("VISUAL", "code --wait")
		t.Setenv("EDITOR", "nano")
		cfg := &Config{}
		got := ResolveEditor(cfg)
		if got != "code --wait" {
			t.Errorf("got %q, want %q", got, "code --wait")
		}
	})

	t.Run("EDITOR when no config or VISUAL", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "nano")
		cfg := &Config{}
		got := ResolveEditor(cfg)
		if got != "nano" {
			t.Errorf("got %q, want %q", got, "nano")
		}
	})

	t.Run("defaults to vi", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		cfg := &Config{}
		got := ResolveEditor(cfg)
		if got != "vi" {
			t.Errorf("got %q, want %q", got, "vi")
		}
	})
}

func TestEditorFieldParsedFromConfig(t *testing.T) {
	writeTestConfig(t, `editor: nvim`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor = %q, want %q", cfg.Editor, "nvim")
	}
}

func TestWorkspacesParsed(t *testing.T) {
	writeTestConfig(t, `
workspaces:
  - name: Work
    color: "#4A9EEF"
  - name: Personal
    color: "#7BC47F"
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(cfg.Workspaces))
	}
	if cfg.Workspaces[0].Name != "Work" || cfg.Workspaces[0].Color != "#4A9EEF" {
		t.Errorf("workspace 0 = %+v", cfg.Workspaces[0])
	}
	if cfg.Workspaces[1].Name != "Personal" || cfg.Workspaces[1].Color != "#7BC47F" {
		t.Errorf("workspace 1 = %+v", cfg.Workspaces[1])
	}
}

func TestWorkspacesAbsent(t *testing.T) {
	writeTestConfig(t, `theme: default`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(cfg.Workspaces))
	}
}

func TestWorkspacesEmpty(t *testing.T) {
	writeTestConfig(t, `workspaces: []`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(cfg.Workspaces))
	}
}

func TestSwarmMaxStepsPerPlanParsed(t *testing.T) {
	writeTestConfig(t, `
swarm:
  max_steps_per_plan: 7
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Swarm.MaxStepsPerPlan != 7 {
		t.Errorf("MaxStepsPerPlan = %d, want 7", cfg.Swarm.MaxStepsPerPlan)
	}
}

func TestMacrosParsed(t *testing.T) {
	writeTestConfig(t, `
macros:
  - name: "run tests"
    keys: "task test\n"
  - name: "git diff"
    keys: "! git diff\n"
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Macros) != 2 {
		t.Fatalf("expected 2 macros, got %d", len(cfg.Macros))
	}
	if cfg.Macros[0].Name != "run tests" {
		t.Errorf("macros[0].Name = %q, want %q", cfg.Macros[0].Name, "run tests")
	}
	if cfg.Macros[0].Keys != "task test\n" {
		t.Errorf("macros[0].Keys = %q, want %q", cfg.Macros[0].Keys, "task test\n")
	}
	if cfg.Macros[1].Name != "git diff" {
		t.Errorf("macros[1].Name = %q, want %q", cfg.Macros[1].Name, "git diff")
	}
}

func TestMacrosEmpty(t *testing.T) {
	writeTestConfig(t, `theme: default`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Macros) != 0 {
		t.Errorf("expected 0 macros, got %d", len(cfg.Macros))
	}
}

func TestMacroEnvVarPreserved(t *testing.T) {
	// Set an env var that would match the macro pattern so we can detect
	// if os.ExpandEnv had run over the macro keys.
	t.Setenv("LEGATO_TASK_ID", "SHOULD_NOT_APPEAR")
	writeTestConfig(t, `
macros:
  - name: "fetch task details"
    keys: "!legato task show $LEGATO_TASK_ID"
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(cfg.Macros))
	}
	want := "!legato task show $LEGATO_TASK_ID"
	if cfg.Macros[0].Keys != want {
		t.Errorf("macro keys = %q, want %q", cfg.Macros[0].Keys, want)
	}
}

func TestNonMacroFieldsStillExpand(t *testing.T) {
	// Other fields should still get env expansion.
	t.Setenv("LEGATO_JIRA_TOKEN", "secret123")
	writeTestConfig(t, `
jira:
  api_token: "${LEGATO_JIRA_TOKEN}"
macros:
  - name: "fetch"
    keys: "$LEGATO_JIRA_TOKEN"
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira.APIToken != "secret123" {
		t.Errorf("jira api_token = %q, want %q", cfg.Jira.APIToken, "secret123")
	}
	// Macro must stay literal despite the same env var being set.
	if cfg.Macros[0].Keys != "$LEGATO_JIRA_TOKEN" {
		t.Errorf("macro keys = %q, want %q", cfg.Macros[0].Keys, "$LEGATO_JIRA_TOKEN")
	}
}

func TestResolveDBPathPrecedence(t *testing.T) {
	t.Run("from config", func(t *testing.T) {
		cfg := &Config{DB: DBConfig{Path: "/tmp/legato-test.db"}}
		got := ResolveDBPath(cfg)
		if got != "/tmp/legato-test.db" {
			t.Errorf("got %q, want %q", got, "/tmp/legato-test.db")
		}
	})

	t.Run("from XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/home/user/.data")
		cfg := &Config{}
		got := ResolveDBPath(cfg)
		want := "/home/user/.data/legato/legato.db"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		cfg := &Config{}
		got := ResolveDBPath(cfg)
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".local", "share", "legato", "legato.db")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestLoadSwarmParticipantProfiles(t *testing.T) {
	writeTestConfig(t, `
swarm:
  conductor:
    agent: chimera
    model: manticore/sol
  worker:
    agent: chimera
    model: manticore/luna
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Swarm.Conductor; got != (SwarmParticipantConfig{Agent: "chimera", Model: "manticore/sol"}) {
		t.Fatalf("conductor = %#v", got)
	}
	if got := cfg.Swarm.Worker; got != (SwarmParticipantConfig{Agent: "chimera", Model: "manticore/luna"}) {
		t.Fatalf("worker = %#v", got)
	}
}

func TestLoadRejectsRemovedSwarmKeys(t *testing.T) {
	for _, tc := range []struct {
		key         string
		replacement string
	}{
		{"default_agent", "swarm.worker.agent"},
		{"conductor_agent", "swarm.conductor.agent"},
		{"conductor_tier", "swarm.conductor.model"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			writeTestConfig(t, "swarm:\n  "+tc.key+": chimera\n")
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.replacement) {
				t.Fatalf("error = %v, want replacement %q", err, tc.replacement)
			}
		})
	}
}

func TestValidateSwarmConfigRejectsInvalidProfiles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile SwarmParticipantConfig
		want    string
	}{
		{"unknown agent", SwarmParticipantConfig{Agent: "other"}, "not a registered adapter"},
		{"blank agent", SwarmParticipantConfig{Agent: "  "}, "agent must not be blank"},
		{"blank model", SwarmParticipantConfig{Model: "  "}, "model must not be blank"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSwarmConfig(&Config{Swarm: SwarmConfig{Worker: tc.profile}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

// contains is a helper to avoid importing strings just for this.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSSHAgentSocketOverride(t *testing.T) {
	// Save and restore SSH_AUTH_SOCK
	original := os.Getenv("SSH_AUTH_SOCK")
	t.Cleanup(func() { os.Setenv("SSH_AUTH_SOCK", original) })

	t.Setenv("SSH_AUTH_SOCK", "")

	writeTestConfig(t, `
ssh_agent_socket: /custom/agent.sock
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSHAgentSocket != "/custom/agent.sock" {
		t.Fatalf("expected SSHAgentSocket=/custom/agent.sock, got %q", cfg.SSHAgentSocket)
	}
	if got := os.Getenv("SSH_AUTH_SOCK"); got != "/custom/agent.sock" {
		t.Fatalf("expected SSH_AUTH_SOCK=/custom/agent.sock, got %q", got)
	}
}

func TestSSHAgentSocketAutoDetect1Password(t *testing.T) {
	original := os.Getenv("SSH_AUTH_SOCK")
	t.Cleanup(func() { os.Setenv("SSH_AUTH_SOCK", original) })

	t.Setenv("SSH_AUTH_SOCK", "")

	// Create a fake 1Password agent socket file
	dir := t.TempDir()
	home := dir
	t.Setenv("HOME", home)
	socketPath := filepath.Join(home, ".1password", "agent.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	writeTestConfig(t, ``)
	_, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("SSH_AUTH_SOCK"); got != socketPath {
		t.Fatalf("expected SSH_AUTH_SOCK=%q, got %q", socketPath, got)
	}
}

func TestSSHAgentSocketNoOverrideWhenAlreadySet(t *testing.T) {
	original := os.Getenv("SSH_AUTH_SOCK")
	t.Cleanup(func() { os.Setenv("SSH_AUTH_SOCK", original) })

	t.Setenv("SSH_AUTH_SOCK", "/existing/agent.sock")

	writeTestConfig(t, `
ssh_agent_socket: /custom/agent.sock
`)
	_, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Explicit config override wins over existing env
	if got := os.Getenv("SSH_AUTH_SOCK"); got != "/custom/agent.sock" {
		t.Fatalf("expected SSH_AUTH_SOCK=/custom/agent.sock, got %q", got)
	}
}

func TestAdapterNativeBridgeIsOnUnlessDisabled(t *testing.T) {
	writeTestConfig(t, `
adapters:
  claude-code:
    launch_args: ["--dangerously-skip-permissions"]
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Adapters["claude-code"].NativeBridge != nil {
		t.Fatalf("native_bridge = %v, want unset so the default applies", *cfg.Adapters["claude-code"].NativeBridge)
	}

	writeTestConfig(t, `
adapters:
  claude-code:
    native_bridge: false
`)
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if disabled := cfg.Adapters["claude-code"].NativeBridge; disabled == nil || *disabled {
		t.Fatalf("native_bridge = %v, want false", disabled)
	}
}

func TestAdapterModelsListTheChoicesOfferedInTheNativePanel(t *testing.T) {
	writeTestConfig(t, `
adapters:
  claude-code:
    models:
      - claude-sonnet-5-5
      - claude-haiku-5-5
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Adapters["claude-code"].Models, ","); got != "claude-sonnet-5-5,claude-haiku-5-5" {
		t.Fatalf("models = %q", got)
	}
}

func TestManticoreUsageReportingLoadsWithItsKeyFromTheEnvironment(t *testing.T) {
	t.Setenv("LEGATO_MANTICORE_KEY", "mc_secret")
	writeTestConfig(t, `
manticore:
  url: http://192.168.1.220:3456/
  api_key: ${LEGATO_MANTICORE_KEY}
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// The trailing slash is the person's habit, not part of the address.
	if cfg.Manticore != (ManticoreConfig{URL: "http://192.168.1.220:3456", APIKey: "mc_secret"}) {
		t.Fatalf("Manticore config = %+v", cfg.Manticore)
	}
}

func TestManticoreUsageReportingIsOffWhenAbsent(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Manticore != (ManticoreConfig{}) {
		t.Fatalf("Manticore config = %+v, want it unset", cfg.Manticore)
	}
}

func TestManticoreUsageReportingRejectsAHalfConfiguredInstance(t *testing.T) {
	t.Setenv("LEGATO_MANTICORE_KEY", "")
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "no key", yaml: "manticore:\n  url: http://manticore.local:3456\n", wantErr: "manticore.api_key"},
		{name: "a key whose variable is unset", yaml: "manticore:\n  url: http://manticore.local:3456\n  api_key: ${LEGATO_MANTICORE_KEY}\n", wantErr: "manticore.api_key"},
		{name: "a key with nowhere to send to", yaml: "manticore:\n  api_key: mc_secret\n", wantErr: "manticore.url"},
		{name: "an address without a scheme", yaml: "manticore:\n  url: manticore.local:3456\n  api_key: mc_secret\n", wantErr: "manticore.url"},
		{name: "an address that is not http", yaml: "manticore:\n  url: ftp://manticore.local\n  api_key: mc_secret\n", wantErr: "manticore.url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeTestConfig(t, tt.yaml)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestAgentsMCPConfig(t *testing.T) {
	load := func(t *testing.T, yaml string) (*Config, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("LEGATO_CONFIG", path)
		return Load()
	}

	t.Run("defaults give agents the tools and a short list of content hosts", func(t *testing.T) {
		t.Setenv("LEGATO_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Agents.MCP.Enabled {
			t.Fatal("agent tools should be on by default")
		}
		want := "cdnjs.cloudflare.com,cdn.jsdelivr.net,unpkg.com,fonts.googleapis.com,fonts.gstatic.com"
		if got := strings.Join(cfg.Agents.MCP.ResourceDomains, ","); got != want {
			t.Fatalf("default resource domains = %q, want %q", got, want)
		}
	})

	t.Run("an unrelated agents setting keeps the defaults", func(t *testing.T) {
		cfg, err := load(t, "agents:\n  peer_awareness: false\n")
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Agents.MCP.Enabled || len(cfg.Agents.MCP.ResourceDomains) != 5 {
			t.Fatalf("mcp config = %#v", cfg.Agents.MCP)
		}
	})

	t.Run("explicit values win, normalised", func(t *testing.T) {
		cfg, err := load(t, "agents:\n  mcp:\n    enabled: false\n    resource_domains: [\" CDN.Example.com \", cdn.example.com, esm.sh]\n")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Agents.MCP.Enabled {
			t.Fatal("agent tools should honor explicit false")
		}
		if got := strings.Join(cfg.Agents.MCP.ResourceDomains, ","); got != "cdn.example.com,esm.sh" {
			t.Fatalf("resource domains = %q", got)
		}
	})

	t.Run("an empty list means no network at all", func(t *testing.T) {
		cfg, err := load(t, "agents:\n  mcp:\n    resource_domains: []\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Agents.MCP.ResourceDomains) != 0 {
			t.Fatalf("resource domains = %v, want none", cfg.Agents.MCP.ResourceDomains)
		}
	})

	for name, value := range map[string]string{
		"a URL":            "https://cdn.example.com",
		"a path":           "cdn.example.com/npm",
		"a wildcard":       "*.example.com",
		"a CSP keyword":    "'unsafe-eval'",
		"several in one":   "cdn.example.com evil.example.com",
		"a port":           "cdn.example.com:8443",
		"a bare top level": "localhost",
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := load(t, "agents:\n  mcp:\n    resource_domains: [\""+value+"\"]\n")
			if err == nil || !strings.Contains(err.Error(), "agents.mcp.resource_domains") {
				t.Fatalf("error = %v, want one naming agents.mcp.resource_domains", err)
			}
		})
	}
}

func TestApprovalNotificationsAreOnByDefaultAndCanBeTurnedOff(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications.ApprovalsEnabled() {
		t.Fatal("approval notifications are off by default")
	}

	writeTestConfig(t, "notifications:\n  approvals: false\nweb:\n  public_url: https://legato.example.net:3080/\n")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.ApprovalsEnabled() {
		t.Fatal("notifications.approvals: false left them on")
	}
	if cfg.Web.PublicURL != "https://legato.example.net:3080" {
		t.Fatalf("public_url = %q, want it without the trailing slash", cfg.Web.PublicURL)
	}

	writeTestConfig(t, "web:\n  public_url: legato.lan\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "web.public_url") {
		t.Fatalf("error = %v, want one about web.public_url", err)
	}
}
