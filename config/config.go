package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/macros"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Jira                   JiraConfig               `yaml:"jira"`
	Board                  BoardConfig              `yaml:"board"`
	Theme                  string                   `yaml:"theme"`
	Icons                  string                   `yaml:"icons"` // "unicode" (default) or "nerdfonts"
	Editor                 string                   `yaml:"editor"`
	Keybindings            KeybindingsConfig        `yaml:"keybindings"`
	DB                     DBConfig                 `yaml:"db"`
	DataDir                string                   `yaml:"data_dir"`
	Agents                 AgentsConfig             `yaml:"agents"`
	GitHub                 GitHubConfig             `yaml:"github"`
	Web                    WebConfig                `yaml:"web"`
	Notifications          NotificationsConfig      `yaml:"notifications"`
	Workspaces             []WorkspaceConfig        `yaml:"workspaces"`
	DefaultWorkspace       string                   `yaml:"default_workspace"`
	DefaultImportWorkspace string                   `yaml:"default_import_workspace"`
	Swarm                  SwarmConfig              `yaml:"swarm"`
	Adapters               map[string]AdapterConfig `yaml:"adapters"`
	Macros                 []macros.Macro           `yaml:"macros"`
	Voice                  VoiceConfig              `yaml:"voice"`
	Worktrees              WorktreesConfig          `yaml:"worktrees"`
	Groups                 GroupsConfig             `yaml:"groups"`
	Workflows              WorkflowsConfig          `yaml:"workflows"`
	Repositories           []RepositoryConfig       `yaml:"repositories"`
	Resources              []ResourceConfig         `yaml:"resources"`
	// Credentials agents can ask for, and where Legato keeps its own secrets.
	Credentials []CredentialConfig `yaml:"credentials"`
	Secrets     SecretsConfig      `yaml:"secrets"`
	// CredentialChannels are where agents reach credentials without holding them.
	CredentialChannels CredentialChannelsConfig `yaml:"credential_channels"`
	Learning           LearningConfig           `yaml:"learning"`
	SSHAgentSocket     string                   `yaml:"ssh_agent_socket"`
	Manticore          ManticoreConfig          `yaml:"manticore"`
}

// ManticoreConfig names the Manticore instance that Claude Code sessions
// report their usage to. Claude Code's requests go straight to its provider,
// so Manticore cannot see them as a proxy; with a URL set, Legato loads a mod
// into each Claude Code session it launches that reports token counts after
// the fact. Unset, nothing is installed or sent.
type ManticoreConfig struct {
	// URL is the instance's base address, e.g. http://192.168.1.220:3456.
	URL string `yaml:"url"`
	// APIKey is the key of the Manticore Client the usage is attributed to.
	APIKey string `yaml:"api_key"`
}

// LearningConfig controls opt-in learning automation and its bounded inputs.
type LearningConfig struct {
	Enabled           bool     `yaml:"enabled"`
	PromptEnabled     bool     `yaml:"prompt_enabled"`
	TransitionColumns []string `yaml:"transition_columns"`
	CuratorAdapter    string   `yaml:"curator_adapter"`
	CuratorTier       string   `yaml:"curator_tier"`
	CuratorWorkingDir string   `yaml:"curator_working_dir"`
	PacketMaxItems    int      `yaml:"packet_max_items"`
	PacketMaxBytes    int      `yaml:"packet_max_bytes"`
	// ReflectionTimeout and ReflectionRetries are reserved for future
	// reflection execution policy. They are validated but currently no-ops.
	ReflectionTimeout string `yaml:"reflection_timeout"`
	ReflectionRetries int    `yaml:"reflection_retries"`
	PromptMaxItems    int    `yaml:"prompt_max_items"`
	PromptMaxBytes    int    `yaml:"prompt_max_bytes"`
	// ConsolidationAutomatic and ConsolidationThreshold are reserved for
	// future automatic consolidation scheduling. They are validated but
	// currently no-ops; consolidation is launched manually.
	ConsolidationAutomatic bool                    `yaml:"consolidation_automatic"`
	ConsolidationThreshold int                     `yaml:"consolidation_threshold"`
	Embedding              LearningEmbeddingConfig `yaml:"embedding"`
}

type LearningEmbeddingConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	BaseURL    string `yaml:"base_url"`
	APIKeyEnv  string `yaml:"api_key_env"`
	Timeout    string `yaml:"timeout"`
	Dimensions int    `yaml:"dimensions"`
	BatchSize  int    `yaml:"batch_size"`
}

// WorkflowsConfig holds workflow launch conveniences.
type WorkflowsConfig struct {
	Repositories []WorkflowRepositoryConfig `yaml:"repositories"`
}

// RepositoryConfig is a shared repository/path preset used by workflows,
// task context discovery, and agent launch surfaces.
type RepositoryConfig struct {
	Name         string   `yaml:"name" json:"name"`
	Repo         string   `yaml:"repo" json:"repo"`
	PrimaryDir   string   `yaml:"primary_dir" json:"primary_dir"`
	BaseBranch   string   `yaml:"base_branch" json:"base_branch,omitempty"`
	JiraProjects []string `yaml:"jira_projects" json:"jira_projects,omitempty"`
	Workspaces   []string `yaml:"workspaces" json:"workspaces,omitempty"`
}

// WorkflowRepositoryConfig is retained only to load legacy configuration.
type WorkflowRepositoryConfig = RepositoryConfig

// GroupsConfig holds task grouping defaults.
type GroupsConfig struct {
	Defaults []string `yaml:"defaults"`
}

// WorktreesConfig holds optional integrations for creating task worktrees.
type WorktreesConfig struct {
	Yggdrasil YggdrasilConfig `yaml:"yggdrasil"`
}

// YggdrasilConfig controls the optional Yggdrasil worktree integration.
type YggdrasilConfig struct {
	Enabled bool `yaml:"enabled"`
}

// AdapterConfig holds per-adapter launch settings (e.g. extra CLI flags
// passed to `claude`, `chimera`, or `codex` when a swarm participant is auto-launched).
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
	// Tiers defines named launch profiles per adapter (e.g. small/medium/large
	// or haiku/sonnet/opus). The conductor picks a tier per sub-task in its
	// plan, and the agent service appends the tier's launch_args after the
	// adapter's base launch_args when spawning the worker. Tier args layered
	// last so a tier-specified flag (typically `--model`) wins on conflicts.
	Tiers map[string]TierConfig `yaml:"tiers"`
	// NativeBridge controls whether sessions of this adapter are connected to
	// Legato beyond their terminal, which lets Legato message, interrupt and
	// approve directly and show the session in the native panel. `claude-code`
	// sessions load the Legato mod; `codex` sessions are started through
	// `legato agent codex`, sharing a Codex app server with Legato's bridge.
	// Unset means enabled; set `native_bridge: false` to keep an adapter's
	// sessions tmux-only. Other adapters ignore it.
	NativeBridge *bool `yaml:"native_bridge"`
	// Models are the model IDs the native panel offers for sessions of this
	// adapter, beside the model each session already runs. Only `claude-code`
	// reads it, and it takes full IDs (`claude-sonnet-5-5`), not the aliases
	// `/model` accepts: a choice is applied to each request as it is sent,
	// where the engine no longer resolves aliases.
	Models []string `yaml:"models"`
}

// TierConfig describes a single named launch profile for an adapter — see
// docs/ai/swarm.md § Tiers for the full ordering and validation rules.
type TierConfig struct {
	// Description is the one-liner the conductor sees when picking a tier.
	Description string `yaml:"description"`
	// LaunchArgs are appended after the adapter's base LaunchArgs.
	LaunchArgs []string `yaml:"launch_args"`
}

// SwarmParticipantConfig selects the adapter and model for a swarm role.
type SwarmParticipantConfig struct {
	Agent string `yaml:"agent"`
	Model string `yaml:"model"`
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
	// Conductor selects the adapter and model for newly spawned conductors.
	// An empty agent falls back to Worker.Agent, then the registered default.
	Conductor SwarmParticipantConfig `yaml:"conductor"`
	// Worker selects the adapter and model for workers without plan overrides.
	Worker SwarmParticipantConfig `yaml:"worker"`
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
	// Inner key: adapter name ("claude-code", "chimera", "codex").
	Prompts map[string]map[string]string `yaml:"prompts"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`  // enable auto-generated HTTPS; explicit cert/key also enable TLS
	Cert     string `yaml:"cert"`     // path to TLS certificate PEM file
	Key      string `yaml:"key"`      // path to TLS private key PEM file
	Hostname string `yaml:"hostname"` // additional DNS name for auto-generated certs (e.g. "mybox.local")
}

// NtfyConfig holds ntfy.sh push notification settings.
type NtfyConfig struct {
	URL   string `yaml:"url"`   // ntfy server URL, e.g. https://ntfy.sh
	Topic string `yaml:"topic"` // topic name to publish to
	Token string `yaml:"token"` // optional access token for private topics
}

// NotificationsConfig holds notification settings.
type NotificationsConfig struct {
	Ntfy NtfyConfig            `yaml:"ntfy"`
	OS   OSNotificationsConfig `yaml:"os"`
	// Approvals sends an ntfy notification for each new approval request.
	// Default true.
	Approvals *bool `yaml:"approvals"`
}

// ApprovalsEnabled reports whether new approvals send a notification.
func (n NotificationsConfig) ApprovalsEnabled() bool {
	return n.Approvals == nil || *n.Approvals
}

// OSNotificationsConfig holds OS-native desktop notification settings.
type OSNotificationsConfig struct {
	Enabled bool `yaml:"enabled"` // enable OS desktop notifications (Linux/macOS)
}

type WebConfig struct {
	Enabled bool      `yaml:"enabled"` // auto-start web server alongside TUI
	Port    string    `yaml:"port"`    // default "3080"
	TLS     TLSConfig `yaml:"tls"`
	// Discovery announces the web server on the local network (mDNS) so the
	// Legato app can find it. Default true.
	Discovery *bool `yaml:"discovery"`
	// PublicURL is the address a phone uses for this server, in the links of
	// approval notifications. Optional: without it Legato uses the first
	// local network address it announces.
	PublicURL string `yaml:"public_url"`
}

type GitHubConfig struct {
	Auth                        string `yaml:"auth"`
	Token                       string `yaml:"token"`
	PollIntervalSeconds         int    `yaml:"poll_interval_seconds"`          // unresolved PRs (branch-only, no PR yet) — default 600s (10 min)
	ResolvedPollIntervalSeconds int    `yaml:"resolved_poll_interval_seconds"` // resolved PRs (have PR number) — default 600s (10 min)
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
	EscapeKey       string            `yaml:"escape_key"`
	TmuxOptions     map[string]string `yaml:"tmux_options"`
	SparklineWindow string            `yaml:"sparkline_window"` // Go duration string, default "10m"
	PeerAwareness   bool              `yaml:"peer_awareness"`   // automatic peer context and join notices; default true
	MCP             AgentMCPConfig    `yaml:"mcp"`
}

// AgentMCPConfig configures the tools Legato gives the agents it launches,
// through the MCP server `legato mcp serve`.
type AgentMCPConfig struct {
	// Enabled adds the server to every agent session Legato launches.
	// Default true.
	Enabled bool `yaml:"enabled"`
	// ResourceDomains are the hosts HTML an agent shows may load scripts,
	// styles, fonts and images from. An empty list allows none.
	ResourceDomains []string `yaml:"resource_domains"`
}

// DefaultAgentResourceDomains are the hosts an agent's HTML may load from
// unless the config says otherwise: the common script and font CDNs.
func DefaultAgentResourceDomains() []string {
	return []string{"cdnjs.cloudflare.com", "cdn.jsdelivr.net", "unpkg.com", "fonts.googleapis.com", "fonts.gstatic.com"}
}

type JiraConfig struct {
	BaseURL                string   `yaml:"base_url"`
	Email                  string   `yaml:"email"`
	APIToken               string   `yaml:"api_token"`
	ProjectKeys            []string `yaml:"project_keys"`
	JQLFilter              string   `yaml:"jql_filter"`
	SyncIntervalSeconds    int      `yaml:"sync_interval_seconds"`
	StatusSyncEnabled      bool     `yaml:"status_sync_enabled"`
	AttachmentMaxSizeBytes int64    `yaml:"attachment_max_size_bytes"`
}

type BoardConfig struct {
	Columns []ColumnConfig `yaml:"columns"`
}

type ColumnConfig struct {
	Name               string   `yaml:"name"`
	RemoteStatuses     []string `yaml:"remote_statuses"`
	RemoteTransitionID string   `yaml:"remote_transition_id"`
}

// VoiceConfig holds optional voice-to-tmux dictation settings. When Enabled
// is false (the default), the voice feature is entirely hidden from the TUI.
type VoiceConfig struct {
	// Enabled gates the voice feature. When false the 'v' keybinding is
	// hidden and the voice service is not constructed.
	Enabled bool `yaml:"enabled"`
	// WhisperURL is the full URL of the faster-whisper (OpenAI-compatible)
	// transcription endpoint, e.g. "http://192.168.1.50:8000/v1/audio/transcriptions".
	WhisperURL string `yaml:"whisper_url"`
	// AutoSend sends the Enter key after the transcribed text so the agent
	// immediately processes the message. When false the text is typed into
	// the pane but not submitted.
	AutoSend bool `yaml:"autosend"`
	// MicDevice is the ALSA device name for audio capture (e.g. "hw:1,0"
	// or "default"). Empty falls back to "default" at runtime.
	MicDevice string `yaml:"mic_device"`
}

type KeybindingsConfig struct {
	VimMode bool `yaml:"vim_mode"`
}

type DBConfig struct {
	Driver string `yaml:"driver"`
	Path   string `yaml:"path"`
}

type ResolvedDatabase struct {
	Driver string
	Path   string
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

	// Capture macros from the raw file before os.ExpandEnv touches $VARs.
	// Macros are sent verbatim to agent tmux panes, so env vars inside
	// macro keys must stay literal.
	rawMacros, err := extractMacrosRaw(data)
	if err != nil {
		return nil, fmt.Errorf("parsing raw macros: %w", err)
	}

	expanded := os.ExpandEnv(string(data))

	if err := validateRemovedSwarmKeys([]byte(expanded)); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, err
	}

	cfg.Macros = rawMacros
	if len(cfg.Repositories) == 0 && len(cfg.Workflows.Repositories) > 0 {
		cfg.Repositories = append([]RepositoryConfig(nil), cfg.Workflows.Repositories...)
	}

	applyDefaults(cfg)
	if cfg.GitHub.Auth != "auto" && cfg.GitHub.Auth != "gh" && cfg.GitHub.Auth != "token" {
		return nil, fmt.Errorf("github.auth must be auto, gh, or token")
	}
	if err := ValidateSwarmConfig(cfg); err != nil {
		return nil, err
	}
	if err := ValidateLearningConfig(cfg); err != nil {
		return nil, err
	}
	if err := ValidateWorkspaceDefaults(cfg); err != nil {
		return nil, err
	}
	if err := normalizeManticoreConfig(cfg); err != nil {
		return nil, err
	}
	if err := normalizeAgentMCPConfig(cfg); err != nil {
		return nil, err
	}
	if err := normalizeResources(cfg); err != nil {
		return nil, err
	}
	if err := normalizeCredentials(cfg); err != nil {
		return nil, err
	}
	if cfg.Web.PublicURL != "" {
		if u, err := url.Parse(cfg.Web.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("web.public_url %q: want an http or https address", cfg.Web.PublicURL)
		}
	}
	return cfg, nil
}

var resourceDomainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// normalizeAgentMCPConfig lower-cases and de-duplicates the hosts an agent's
// HTML may load from, and rejects anything that is not a plain host name.
// The list becomes part of a content security policy in the browser, so a
// scheme, a wildcard or a policy keyword must never get through.
func normalizeAgentMCPConfig(cfg *Config) error {
	domains := make([]string, 0, len(cfg.Agents.MCP.ResourceDomains))
	seen := map[string]bool{}
	for _, entry := range cfg.Agents.MCP.ResourceDomains {
		domain := strings.ToLower(strings.TrimSpace(entry))
		if !resourceDomainPattern.MatchString(domain) {
			return fmt.Errorf("agents.mcp.resource_domains: %q is not a host name such as cdn.jsdelivr.net", entry)
		}
		if !seen[domain] {
			seen[domain] = true
			domains = append(domains, domain)
		}
	}
	cfg.Agents.MCP.ResourceDomains = domains
	return nil
}

// normalizeManticoreConfig trims the Manticore settings and rejects an
// instance that is only half described: reporting needs both where to send
// and the key that says whose usage it is.
func normalizeManticoreConfig(cfg *Config) error {
	manticore := &cfg.Manticore
	manticore.URL = strings.TrimRight(strings.TrimSpace(manticore.URL), "/")
	manticore.APIKey = strings.TrimSpace(manticore.APIKey)
	if manticore.URL == "" && manticore.APIKey == "" {
		return nil
	}
	if manticore.URL == "" {
		return fmt.Errorf("manticore.url is required when manticore.api_key is set")
	}
	parsed, err := url.Parse(manticore.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("manticore.url must be an http or https address, got %q", manticore.URL)
	}
	if manticore.APIKey == "" {
		return fmt.Errorf("manticore.api_key is required when manticore.url is set")
	}
	return nil
}

// ValidateWorkspaceDefaults checks workspace identities and configured default references.
func ValidateWorkspaceDefaults(cfg *Config) error {
	workspaceNames := make(map[string]string, len(cfg.Workspaces))
	for _, workspace := range cfg.Workspaces {
		name := strings.TrimSpace(workspace.Name)
		if name == "" {
			return fmt.Errorf("workspace name must not be blank")
		}
		key := strings.ToLower(name)
		if _, exists := workspaceNames[key]; exists {
			return fmt.Errorf("duplicate workspace name %q", workspace.Name)
		}
		workspaceNames[key] = name
	}

	for key, name := range map[string]string{
		"default_workspace":        cfg.DefaultWorkspace,
		"default_import_workspace": cfg.DefaultImportWorkspace,
	} {
		if name == "" {
			continue
		}
		if _, exists := workspaceNames[strings.ToLower(strings.TrimSpace(name))]; !exists {
			return fmt.Errorf("%s %q does not match a configured workspace", key, name)
		}
	}
	return nil
}

// extractMacrosRaw unmarshals only the macros field from raw YAML bytes,
// skipping any env-var expansion so $VAR references stay literal.
func extractMacrosRaw(data []byte) ([]macros.Macro, error) {
	var raw struct {
		Macros []macros.Macro `yaml:"macros"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw.Macros, nil
}

var supportedSwarmAgents = map[string]struct{}{
	"claude-code": {},
	"chimera":     {},
	"codex":       {},
}

func validateRemovedSwarmKeys(data []byte) error {
	var raw struct {
		Swarm map[string]yaml.Node `yaml:"swarm"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}
	replacements := map[string]string{
		"default_agent":   "swarm.worker.agent",
		"conductor_agent": "swarm.conductor.agent",
		"conductor_tier":  "swarm.conductor.model",
	}
	for _, key := range []string{"default_agent", "conductor_agent", "conductor_tier"} {
		if _, ok := raw.Swarm[key]; ok {
			return fmt.Errorf("swarm.%s was removed; use %s instead", key, replacements[key])
		}
	}
	return nil
}

// ValidateSwarmConfig validates the nested conductor and worker profiles.
func ValidateSwarmConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	profiles := []struct {
		name    string
		profile SwarmParticipantConfig
	}{
		{"conductor", cfg.Swarm.Conductor},
		{"worker", cfg.Swarm.Worker},
	}
	for _, item := range profiles {
		if item.profile.Agent != "" && strings.TrimSpace(item.profile.Agent) == "" {
			return fmt.Errorf("swarm.%s.agent must not be blank", item.name)
		}
		if item.profile.Model != "" && strings.TrimSpace(item.profile.Model) == "" {
			return fmt.Errorf("swarm.%s.model must not be blank", item.name)
		}
		if item.profile.Agent != "" {
			if _, ok := supportedSwarmAgents[item.profile.Agent]; !ok {
				return fmt.Errorf("swarm.%s.agent %q is not a registered adapter", item.name, item.profile.Agent)
			}
		}
	}
	return nil
}

// ValidateLearningConfig validates bounded learning settings and, when enabled,
// verifies trigger columns and curator references against the loaded config.
func ValidateLearningConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	learning := cfg.Learning
	positive := []struct {
		name  string
		value int
	}{
		{"learning.packet_max_items", learning.PacketMaxItems},
		{"learning.packet_max_bytes", learning.PacketMaxBytes},
		{"learning.reflection_retries", learning.ReflectionRetries},
		{"learning.prompt_max_items", learning.PromptMaxItems},
		{"learning.prompt_max_bytes", learning.PromptMaxBytes},
		{"learning.consolidation_threshold", learning.ConsolidationThreshold},
	}
	for _, limit := range positive {
		if limit.value <= 0 {
			return fmt.Errorf("%s must be positive", limit.name)
		}
	}
	timeout, err := time.ParseDuration(learning.ReflectionTimeout)
	if err != nil || timeout <= 0 {
		return fmt.Errorf("learning.reflection_timeout must be a positive duration")
	}

	if learning.Embedding.Enabled {
		if learning.Embedding.Provider != "openai-compatible" {
			return fmt.Errorf("learning.embedding.provider must be openai-compatible")
		}
		if strings.TrimSpace(learning.Embedding.Model) == "" || strings.TrimSpace(learning.Embedding.BaseURL) == "" {
			return fmt.Errorf("learning.embedding.model and base_url are required when embeddings are enabled")
		}
		if timeout, err := time.ParseDuration(learning.Embedding.Timeout); err != nil || timeout <= 0 {
			return fmt.Errorf("learning.embedding.timeout must be a positive duration")
		}
		if learning.Embedding.Dimensions < 0 || learning.Embedding.BatchSize <= 0 {
			return fmt.Errorf("learning.embedding.dimensions must not be negative and batch_size must be positive")
		}
	}

	if learning.CuratorWorkingDir != "" && !filepath.IsAbs(learning.CuratorWorkingDir) {
		return fmt.Errorf("learning.curator_working_dir must be an absolute path")
	}
	if !learning.Enabled {
		return nil
	}
	if len(learning.TransitionColumns) == 0 {
		return fmt.Errorf("learning.transition_columns must contain at least one board column when learning is enabled")
	}
	columns := make(map[string]struct{}, len(cfg.Board.Columns))
	for _, column := range cfg.Board.Columns {
		columns[column.Name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(learning.TransitionColumns))
	for _, column := range learning.TransitionColumns {
		if strings.TrimSpace(column) == "" {
			return fmt.Errorf("learning.transition_columns must not contain an empty column")
		}
		if _, ok := columns[column]; !ok {
			return fmt.Errorf("learning.transition_columns contains unknown board column %q", column)
		}
		if _, ok := seen[column]; ok {
			return fmt.Errorf("learning.transition_columns contains duplicate board column %q", column)
		}
		seen[column] = struct{}{}
	}
	adapter := learning.CuratorAdapter
	if adapter == "" {
		adapter = cfg.Swarm.Worker.Agent
	}
	if adapter == "" {
		return fmt.Errorf("learning.curator_adapter or swarm.worker.agent is required when learning is enabled")
	}
	adapterConfig, ok := cfg.Adapters[adapter]
	if !ok {
		return fmt.Errorf("learning.curator_adapter %q is not configured", adapter)
	}
	if tier := learning.CuratorTier; tier != "" {
		if _, ok := adapterConfig.Tiers[tier]; !ok {
			return fmt.Errorf("learning.curator_tier %q is not configured for adapter %q", tier, adapter)
		}
	}
	return nil
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

func ResolveDatabase(cfg *Config) (ResolvedDatabase, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.DB.Driver))
	if driver == "" {
		driver = "sqlite"
	}
	if driver != "sqlite" {
		return ResolvedDatabase{}, fmt.Errorf("database driver %q is not supported", driver)
	}
	return ResolvedDatabase{Driver: driver, Path: ResolveDBPath(cfg)}, nil
}

func ResolveDataDir(cfg *Config) string {
	if cfg.DataDir != "" {
		return cfg.DataDir
	}
	return filepath.Dir(ResolveDBPath(cfg))
}

func defaults() *Config {
	return &Config{
		Jira: JiraConfig{
			SyncIntervalSeconds:    60,
			StatusSyncEnabled:      true,
			AttachmentMaxSizeBytes: 25 * 1024 * 1024,
		},
		Board: BoardConfig{
			Columns: DefaultColumns(),
		},
		Theme: "default",
		Keybindings: KeybindingsConfig{
			VimMode: true,
		},
		Agents: AgentsConfig{PeerAwareness: true, MCP: AgentMCPConfig{Enabled: true, ResourceDomains: DefaultAgentResourceDomains()}},
		Learning: LearningConfig{
			PacketMaxItems:         100,
			PacketMaxBytes:         256 * 1024,
			ReflectionTimeout:      "5m",
			ReflectionRetries:      3,
			PromptMaxItems:         20,
			PromptMaxBytes:         16 * 1024,
			ConsolidationThreshold: 50,
			Embedding:              LearningEmbeddingConfig{Provider: "openai-compatible", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY", Timeout: "30s", BatchSize: 32},
		},
	}
}

func applyDefaults(cfg *Config) {
	if cfg.GitHub.Auth == "" {
		cfg.GitHub.Auth = "auto"
	}
	if cfg.Jira.SyncIntervalSeconds == 0 {
		cfg.Jira.SyncIntervalSeconds = 60
	}
	if cfg.Jira.AttachmentMaxSizeBytes == 0 {
		cfg.Jira.AttachmentMaxSizeBytes = 25 * 1024 * 1024
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
	if cfg.Agents.SparklineWindow == "" {
		cfg.Agents.SparklineWindow = "10m"
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
	if cfg.Notifications.Ntfy.URL == "" {
		cfg.Notifications.Ntfy.URL = "https://ntfy.sh"
	}
	applySSHAgent(cfg)
	cfg.Web.PublicURL = strings.TrimRight(strings.TrimSpace(cfg.Web.PublicURL), "/")
	// VimMode defaults to true — yaml unmarshals missing bool as false,
	// so we only set it if the entire keybindings section was absent.
	// Since yaml zero-value for bool is false, we handle this in defaults().
}

// applySSHAgent sets SSH_AUTH_SOCK in the process environment when an
// explicit override is configured, or when the env var is empty and a
// well-known agent socket (1Password) exists. This ensures spawned
// subprocesses (git, tmux sessions, workflow steps) can reach the SSH
// agent even when Legato's environment doesn't inherit it from the
// desktop session.
func applySSHAgent(cfg *Config) {
	if cfg.SSHAgentSocket != "" {
		os.Setenv("SSH_AUTH_SOCK", cfg.SSHAgentSocket)
		return
	}
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		return
	}
	// Auto-detect 1Password's agent socket as a fallback
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	socket := filepath.Join(home, ".1password", "agent.sock")
	if info, err := os.Stat(socket); err == nil && !info.IsDir() {
		os.Setenv("SSH_AUTH_SOCK", socket)
	}
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
