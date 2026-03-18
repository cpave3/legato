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
	Keybindings KeybindingsConfig `yaml:"keybindings"`
	DB          DBConfig          `yaml:"db"`
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
	Name              string   `yaml:"name"`
	JiraStatuses      []string `yaml:"jira_statuses"`
	JiraTransitionID  string   `yaml:"jira_transition_id"`
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
			Columns: defaultColumns(),
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
		cfg.Board.Columns = defaultColumns()
	}
	// VimMode defaults to true — yaml unmarshals missing bool as false,
	// so we only set it if the entire keybindings section was absent.
	// Since yaml zero-value for bool is false, we handle this in defaults().
}

func defaultColumns() []ColumnConfig {
	return []ColumnConfig{
		{Name: "Backlog", JiraStatuses: []string{"To Do", "Open", "Backlog"}},
		{Name: "Ready", JiraStatuses: []string{"Ready for Dev", "Selected for Development"}},
		{Name: "Doing", JiraStatuses: []string{"In Progress", "In Development"}},
		{Name: "Review", JiraStatuses: []string{"In Review"}},
		{Name: "Done", JiraStatuses: []string{"Done", "Closed"}},
	}
}
