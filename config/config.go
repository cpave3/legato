package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Jira        JiraConfig        `yaml:"jira"`
	Board       BoardConfig       `yaml:"board"`
	Theme       string            `yaml:"theme"`
	Icons       string            `yaml:"icons"` // "unicode" (default) or "nerdfonts"
	Editor      string            `yaml:"editor"`
	Keybindings KeybindingsConfig `yaml:"keybindings"`
	DB          DBConfig          `yaml:"db"`
	Agents      AgentsConfig      `yaml:"agents"`
	GitHub      GitHubConfig      `yaml:"github"`
	Web         WebConfig         `yaml:"web"`
	Workspaces  []WorkspaceConfig `yaml:"workspaces"`
}

type WebConfig struct {
	Enabled bool   `yaml:"enabled"` // auto-start web server alongside TUI
	Port    string `yaml:"port"`    // default "3080"
}

type GitHubConfig struct {
	PollIntervalSeconds         int `yaml:"poll_interval_seconds"`          // unresolved PRs (branch-only, no PR yet) — default 600s (10 min)
	ResolvedPollIntervalSeconds int `yaml:"resolved_poll_interval_seconds"` // resolved PRs (have PR number) — default 600s (10 min)
}

type WorkspaceConfig struct {
	Name  string `yaml:"name"`
	Color string `yaml:"color"`
}

type AgentsConfig struct {
	EscapeKey   string            `yaml:"escape_key"`
	TmuxOptions map[string]string `yaml:"tmux_options"`
}

type JiraConfig struct {
	BaseURL              string   `yaml:"base_url"`
	Email                string   `yaml:"email"`
	APIToken             string   `yaml:"api_token"`
	ProjectKeys          []string `yaml:"project_keys"`
	JQLFilter            string   `yaml:"jql_filter"`
	SyncIntervalSeconds  int      `yaml:"sync_interval_seconds"`
}

type BoardConfig struct {
	Columns []ColumnConfig `yaml:"columns"`
}

type ColumnConfig struct {
	Name               string   `yaml:"name"`
	RemoteStatuses     []string `yaml:"remote_statuses"`
	RemoteTransitionID string   `yaml:"remote_transition_id"`
}

type KeybindingsConfig struct {
	VimMode bool `yaml:"vim_mode"`
}

type DBConfig struct {
	Path string `yaml:"path"`
}

// ResolveConfigPath returns the config file path using precedence:
// 1. $LEGATO_CONFIG  2. $XDG_CONFIG_HOME/legato/config.yaml  3. ~/.config/legato/config.yaml
func ResolveConfigPath() string {
	if p := os.Getenv("LEGATO_CONFIG"); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "legato", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "legato", "config.yaml")
}

// Load reads and parses the config file, expanding env vars and applying defaults.
// Returns a default config without error if the file does not exist.
func Load() (*Config, error) {
	path := ResolveConfigPath()

	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}

	expanded := os.ExpandEnv(string(data))

	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, err
	}

	applyDefaults(cfg)
	return cfg, nil
}

// ResolveEditor returns the editor command using precedence:
// 1. cfg.Editor  2. $VISUAL  3. $EDITOR  4. vi
func ResolveEditor(cfg *Config) string {
	if cfg.Editor != "" {
		return cfg.Editor
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

// ResolveDBPath returns the database file path using precedence:
// 1. cfg.DB.Path  2. $XDG_DATA_HOME/legato/legato.db  3. ~/.local/share/legato/legato.db
func ResolveDBPath(cfg *Config) string {
	if cfg.DB.Path != "" {
		return cfg.DB.Path
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "legato", "legato.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "legato", "legato.db")
}

func defaults() *Config {
	return &Config{
		Jira: JiraConfig{
			SyncIntervalSeconds: 60,
		},
		Board: BoardConfig{
			Columns: DefaultColumns(),
		},
		Theme: "default",
		Keybindings: KeybindingsConfig{
			VimMode: true,
		},
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Jira.SyncIntervalSeconds == 0 {
		cfg.Jira.SyncIntervalSeconds = 60
	}
	if cfg.Theme == "" {
		cfg.Theme = "default"
	}
	if len(cfg.Board.Columns) == 0 {
		cfg.Board.Columns = DefaultColumns()
	}
	if cfg.Agents.EscapeKey == "" {
		cfg.Agents.EscapeKey = "ctrl+]"
	}
	if cfg.GitHub.PollIntervalSeconds == 0 {
		cfg.GitHub.PollIntervalSeconds = 600
	}
	if cfg.GitHub.ResolvedPollIntervalSeconds == 0 {
		cfg.GitHub.ResolvedPollIntervalSeconds = 600
	}
	if cfg.Web.Port == "" {
		cfg.Web.Port = "3080"
	}
	// VimMode defaults to true — yaml unmarshals missing bool as false,
	// so we only set it if the entire keybindings section was absent.
	// Since yaml zero-value for bool is false, we handle this in defaults().
}

// DefaultColumns returns the default board columns.
func DefaultColumns() []ColumnConfig {
	return []ColumnConfig{
		{Name: "Backlog", RemoteStatuses: []string{"To Do", "Open", "Backlog"}},
		{Name: "Ready", RemoteStatuses: []string{"Ready for Dev", "Selected for Development"}},
		{Name: "Doing", RemoteStatuses: []string{"In Progress", "In Development"}},
		{Name: "Review", RemoteStatuses: []string{"In Review"}},
		{Name: "Done", RemoteStatuses: []string{"Done", "Closed"}},
	}
}
