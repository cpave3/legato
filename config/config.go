package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Jira        JiraConfig                `yaml:"jira"`
	Board       BoardConfig               `yaml:"board"`
	Theme       string                    `yaml:"theme"`
	Icons       string                    `yaml:"icons"` // "unicode" (default) or "nerdfonts"
	Editor      string                    `yaml:"editor"`
	Keybindings KeybindingsConfig         `yaml:"keybindings"`
	DB          DBConfig                  `yaml:"db"`
	Agents      AgentsConfig              `yaml:"agents"`
	GitHub      GitHubConfig              `yaml:"github"`
	Web         WebConfig                 `yaml:"web"`
	Workspaces  []WorkspaceConfig         `yaml:"workspaces"`
	Swarm       SwarmConfig               `yaml:"swarm"`
	Adapters    map[string]AdapterConfig  `yaml:"adapters"`
}

// AdapterConfig holds per-adapter launch settings (e.g. extra CLI flags
// passed to `claude` or `chimera` when a swarm participant is auto-launched).
type AdapterConfig struct {
	// LaunchArgs are appended to the adapter's auto-launch command, after
	// the role-prompt flag. Use this to enable adapter-specific modes (e.g.
	// `["--sandbox"]` for chimera).
	LaunchArgs []string `yaml:"launch_args"`
	// Modes maps swarm role labels to adapter-specific mode names that the
	// adapter activates at launch. The key is the role label
	// ("conductor" or any free-form worker role like "backend"); the value
	// is the adapter's mode name. Adapters that don't have a "mode"
	// concept ignore this field.
	//
	// Resolution: exact role match wins; otherwise non-conductor roles fall
	// back to the "worker" key. When the entire field is unset, adapters
	// use their built-in defaults (e.g. `legato-orchestrator` /
	// `legato-worker` for Chimera). To disable mode injection entirely,
	// set the field to an empty map (`modes: {}`).
	Modes map[string]string `yaml:"modes"`
}

// SwarmConfig holds swarm-orchestration settings.
type SwarmConfig struct {
	// MaxConcurrentAgents caps the number of live workers per swarm.
	// Zero or negative falls back to 4 at runtime.
	MaxConcurrentAgents int `yaml:"max_concurrent_agents"`
	// MaxSubtasksPerPlan caps the size of a single plan submission.
	// Zero or negative falls back to 10 at runtime.
	MaxSubtasksPerPlan int `yaml:"max_subtasks_per_plan"`
	// MaxStepsPerPlan caps the number of steps in a single plan.
	// Zero or negative falls back to 10 at runtime.
	MaxStepsPerPlan int `yaml:"max_steps_per_plan"`
	// DefaultAgent is the adapter used for swarm participants when a plan
	// entry doesn't specify one (e.g. "claude-code").
	DefaultAgent string `yaml:"default_agent"`
	// StrictScope makes scope-overlap conflicts hard-block dispatch instead
	// of advisory.
	StrictScope bool `yaml:"strict_scope"`
	// RequireUserClose adds a HITL gate at every `legato swarm close`.
	// Reserved for future use; currently a no-op.
	RequireUserClose bool `yaml:"require_user_close"`
	// BriefKickoffDelayMs is the delay (in ms) between auto-launch and the
	// "read your brief" send-keys kickoff. Tune up if your AI tool boots
	// slowly enough that the kickoff lands during boot. Default 250ms.
	BriefKickoffDelayMs int `yaml:"brief_kickoff_delay_ms"`
	// Prompts allows overriding role system prompts per adapter.
	// Outer key: free-form role label ("conductor", "backend", etc.).
	// Inner key: adapter name ("claude-code", "chimera").
	Prompts map[string]map[string]string `yaml:"prompts"`
}

type TLSConfig struct {
	Cert     string `yaml:"cert"`     // path to TLS certificate PEM file
	Key      string `yaml:"key"`      // path to TLS private key PEM file
	Hostname string `yaml:"hostname"` // additional DNS name for auto-generated certs (e.g. "mybox.local")
}

type WebConfig struct {
	Enabled bool      `yaml:"enabled"` // auto-start web server alongside TUI
	Port    string    `yaml:"port"`    // default "3080"
	TLS     TLSConfig `yaml:"tls"`
}

type GitHubConfig struct {
	PollIntervalSeconds         int `yaml:"poll_interval_seconds"`          // unresolved PRs (branch-only, no PR yet) — default 600s (10 min)
	ResolvedPollIntervalSeconds int `yaml:"resolved_poll_interval_seconds"` // resolved PRs (have PR number) — default 600s (10 min)
}

type WorkspaceConfig struct {
	Name  string `yaml:"name"`
	Color string `yaml:"color"`
	// Path is the filesystem location associated with this workspace.
	// When set, the swarm-init overlay pre-fills the working directory from
	// the selected card's workspace path.
	Path string `yaml:"path,omitempty"`
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
