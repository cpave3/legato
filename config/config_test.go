package config

import (
	"os"
	"path/filepath"
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

func TestValidateConductorTierEmpty(t *testing.T) {
	cases := []struct {
		name string
		cfg  *Config
	}{
		{"nil cfg", nil},
		{"empty tier", &Config{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateConductorTier(tc.cfg); err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		})
	}
}

func TestValidateConductorTierKnownPasses(t *testing.T) {
	cfg := &Config{
		Swarm: SwarmConfig{DefaultAgent: "claude-code", ConductorTier: "large"},
		Adapters: map[string]AdapterConfig{
			"claude-code": {Tiers: map[string]TierConfig{
				"small": {LaunchArgs: []string{"--model", "haiku"}},
				"large": {LaunchArgs: []string{"--model", "opus"}},
			}},
		},
	}
	if err := ValidateConductorTier(cfg); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestValidateConductorTierUnknownRejected(t *testing.T) {
	cfg := &Config{
		Swarm: SwarmConfig{DefaultAgent: "claude-code", ConductorTier: "ghost"},
		Adapters: map[string]AdapterConfig{
			"claude-code": {Tiers: map[string]TierConfig{
				"small": {LaunchArgs: []string{"--model", "haiku"}},
			}},
		},
	}
	err := ValidateConductorTier(cfg)
	if err == nil {
		t.Fatal("expected error for unknown tier")
	}
	msg := err.Error()
	if !contains(msg, "ghost") || !contains(msg, "claude-code") {
		t.Errorf("error should name the bad tier and adapter: %q", msg)
	}
}

func TestValidateConductorTierMissingDefaultAgentRejected(t *testing.T) {
	cfg := &Config{
		Swarm:    SwarmConfig{ConductorTier: "large"},
		Adapters: map[string]AdapterConfig{},
	}
	err := ValidateConductorTier(cfg)
	if err == nil {
		t.Fatal("expected error when default_agent and conductor_agent unset and tier set")
	}
	msg := err.Error()
	if !contains(msg, "conductor_agent") || !contains(msg, "default_agent") {
		t.Errorf("error should mention both missing fields: %q", msg)
	}
}

func TestValidateConductorTierConductorAgentAloneSuffices(t *testing.T) {
	cfg := &Config{
		Swarm: SwarmConfig{ConductorAgent: "chimera", ConductorTier: "heavy"},
		Adapters: map[string]AdapterConfig{
			"chimera": {Tiers: map[string]TierConfig{
				"heavy": {LaunchArgs: []string{"--model", "opus"}},
			}},
		},
	}
	if err := ValidateConductorTier(cfg); err != nil {
		t.Errorf("expected nil error when conductor_agent is set without default_agent, got %v", err)
	}
}

func TestValidateConductorTierConductorAgentPriority(t *testing.T) {
	cfg := &Config{
		Swarm: SwarmConfig{
			DefaultAgent:   "claude-code",
			ConductorAgent: "chimera",
			ConductorTier:  "heavy",
		},
		Adapters: map[string]AdapterConfig{
			"claude-code": {Tiers: map[string]TierConfig{
				"small": {LaunchArgs: []string{"--model", "haiku"}},
			}},
			"chimera": {Tiers: map[string]TierConfig{
				"heavy": {LaunchArgs: []string{"--model", "opus"}},
			}},
		},
	}
	// Tier "heavy" exists only under chimera — conductor_agent should win
	if err := ValidateConductorTier(cfg); err != nil {
		t.Errorf("expected nil error when conductor_agent wins, got %v", err)
	}
}

func TestValidateConductorTierCodexPasses(t *testing.T) {
	cfg := &Config{
		Swarm: SwarmConfig{ConductorAgent: "codex", ConductorTier: "large"},
		Adapters: map[string]AdapterConfig{
			"codex": {Tiers: map[string]TierConfig{
				"small": {LaunchArgs: []string{"--model", "gpt-4o-mini"}},
				"large": {LaunchArgs: []string{"--model", "gpt-4o"}},
			}},
		},
	}
	if err := ValidateConductorTier(cfg); err != nil {
		t.Errorf("expected nil error for codex tier, got %v", err)
	}
}

func TestValidateConductorTierCodexUnknownTierRejected(t *testing.T) {
	cfg := &Config{
		Swarm: SwarmConfig{ConductorAgent: "codex", ConductorTier: "ghost"},
		Adapters: map[string]AdapterConfig{
			"codex": {Tiers: map[string]TierConfig{
				"small": {LaunchArgs: []string{"--model", "gpt-4o-mini"}},
			}},
		},
	}
	err := ValidateConductorTier(cfg)
	if err == nil {
		t.Fatal("expected error for unknown codex tier")
	}
	msg := err.Error()
	if !contains(msg, "ghost") || !contains(msg, "codex") {
		t.Errorf("error should name the bad tier and adapter: %q", msg)
	}
}

func TestValidateConductorTierAdapterWithoutTiersRejected(t *testing.T) {
	cfg := &Config{
		Swarm: SwarmConfig{DefaultAgent: "claude-code", ConductorTier: "large"},
		Adapters: map[string]AdapterConfig{
			"claude-code": {LaunchArgs: []string{"--foo"}},
		},
	}
	if err := ValidateConductorTier(cfg); err == nil {
		t.Fatal("expected error when adapter has no tiers")
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
