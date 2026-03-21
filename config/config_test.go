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
