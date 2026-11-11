package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// The settings registry: every setting that can live in the database as
// well as in config.yaml, by the same dotted key. A value in config.yaml
// pins its setting; otherwise the database's value applies, else the
// default. See docs/ai/settings.md.

// SettingClass is how much a change to a setting needs (docs/ai/reeve.md
// § Classes): read is free, change needs an approval, risky can need a
// signed one.
type SettingClass string

const (
	ClassRead   SettingClass = "read"
	ClassChange SettingClass = "change"
	ClassRisky  SettingClass = "risky"
)

// SettingMode is when a change takes effect: live (read on each use), new
// agents (agents started after it), or restart (wired into services at
// start).
type SettingMode string

const (
	ModeLive      SettingMode = "live"
	ModeNewAgents SettingMode = "new_agents"
	ModeRestart   SettingMode = "restart"
)

// SettingKind is a setting's type.
type SettingKind string

const (
	KindBool     SettingKind = "bool"
	KindString   SettingKind = "string"
	KindInt      SettingKind = "int"
	KindEnum     SettingKind = "enum"
	KindDuration SettingKind = "duration"
	KindStrings  SettingKind = "strings"
)

// Setting declares one setting that can live in the database.
type Setting struct {
	Key         string       `json:"key"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Kind        SettingKind  `json:"kind"`
	Enum        []string     `json:"enum,omitempty"`
	Class       SettingClass `json:"class"`
	Mode        SettingMode  `json:"mode"`
	// Secret: the value is the name of a secret in Legato's secret store
	// or 1Password, never the secret itself.
	Secret bool `json:"secret,omitempty"`
	// OwnRule marks one of the Reeve's own rules: changed only with a
	// signed approval, and refused when no device is enrolled.
	OwnRule bool `json:"own_rule,omitempty"`
	// Check is the setting's own rule, past its kind and enum.
	Check func(v any) error `json:"-"`
}

var burnliteProjectPattern = regexp.MustCompile(`^[A-Z]{2,6}[0-9]{0,4}$`)

var registry = []Setting{
	// Agents.
	{Key: "agents.escape_key", Title: "Detach key", Description: "The key that leaves an agent's terminal in the TUI, as ctrl+].", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.sparkline_window", Title: "Activity window", Description: "How far back the activity bar of an agent reaches.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.peer_awareness", Title: "Peer awareness", Description: "Tell agents in the same Git worktree about each other.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.reviews", Title: "Reviews", Description: "Ask agents for a review tour and a handoff packet at the end of their work.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.mcp.enabled", Title: "Legato tools", Description: "Give agents Legato's tools (cards, artifacts, notes) through `legato mcp serve`.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.sandbox.enabled", Title: "Sandbox", Description: "Run every agent in Legato's sandbox unless its spawn opts out.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.backend", Title: "Sandbox backend", Description: "Only bwrap for now.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.default_mounts", Title: "Toolchains in the sandbox", Description: "Show the person's toolchains (read-only) and package caches inside the sandbox.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.scratch_dir", Title: "Sandbox scratch folder", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},

	// Notifications.
	{Key: "notifications.ntfy.url", Title: "ntfy server", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.ntfy.topic", Title: "ntfy topic", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.os.enabled", Title: "Desktop notifications", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.approvals", Title: "Notify of approvals", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.handoff", Title: "Notify of handoffs", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},

	// Look and feel.
	{Key: "theme", Title: "Theme", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "icons", Title: "Icons", Description: "Unicode glyphs, or Nerd Font ones.", Kind: KindEnum, Enum: []string{"unicode", "nerdfonts"}, Class: ClassChange, Mode: ModeRestart},
	{Key: "editor", Title: "Editor", Description: "The editor the TUI opens; $VISUAL, then $EDITOR, when empty.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "web.tint", Title: "Tint", Description: "A colour that tells this Legato from others in the web UI.", Kind: KindEnum, Enum: []string{"", "red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}, Class: ClassChange, Mode: ModeLive},

	// Updates.
	{Key: "updates.mode", Title: "Updates", Description: "auto installs new releases; ask offers them; off never looks.", Kind: KindEnum, Enum: []string{"auto", "ask", "off"}, Class: ClassRisky, Mode: ModeLive},

	{Key: "notifications.ntfy.token", Title: "ntfy token", Description: "The name of a secret holding the token for a private topic.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "keybindings.vim_mode", Title: "Vim keys", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "web.discovery", Title: "Announce on the network", Description: "Announce the web server over mDNS so the phone app finds it.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.mcp.resource_domains", Title: "Hosts agents' HTML may load from", Kind: KindStrings, Class: ClassRisky, Mode: ModeRestart},
	{Key: "groups.defaults", Title: "Default groups", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "default_workspace", Title: "Default workspace", Description: "The workspace of a new task.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "default_import_workspace", Title: "Default import workspace", Description: "The workspace of an imported task.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "ssh_agent_socket", Title: "SSH agent socket", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},

	// Jira.
	{Key: "jira.base_url", Title: "Jira address", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.email", Title: "Jira email", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.api_token", Title: "Jira API token", Description: "The name of a secret holding the token.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "jira.project_keys", Title: "Jira projects", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.jql_filter", Title: "Jira filter", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.sync_interval_seconds", Title: "Jira sync interval (seconds)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},
	{Key: "jira.status_sync_enabled", Title: "Move Jira issues with their cards", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.attachment_max_size_bytes", Title: "Largest Jira attachment (bytes)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},

	// Burnlite.
	{Key: "burnlite.url", Title: "Burnlite address", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.sync_interval_seconds", Title: "Burnlite sync interval (seconds)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},
	{Key: "burnlite.status_sync_enabled", Title: "Move cards with Burnlite", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.ready", Title: "Burnlite: column for ready", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.blocked", Title: "Burnlite: column for blocked", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.claimed", Title: "Burnlite: column for claimed", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.closed", Title: "Burnlite: column for closed", Kind: KindString, Class: ClassChange, Mode: ModeRestart},

	// GitHub.
	{Key: "github.auth", Title: "GitHub sign-in", Description: "auto, gh or token.", Kind: KindEnum, Enum: []string{"auto", "gh", "token"}, Class: ClassRisky, Mode: ModeRestart},
	{Key: "github.token", Title: "GitHub token", Description: "The name of a secret holding the token.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "github.poll_interval_seconds", Title: "GitHub poll interval (seconds)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},
	{Key: "github.resolved_poll_interval_seconds", Title: "GitHub poll interval for known PRs (seconds)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},

	// Swarms.
	{Key: "swarm.enabled", Title: "Swarms", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.max_concurrent_agents", Title: "Swarm: most agents at once", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.max_subtasks_per_plan", Title: "Swarm: most sub-tasks in a plan", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.max_steps_per_plan", Title: "Swarm: most steps in a plan", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.conductor.agent", Title: "Swarm conductor's agent tool", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.conductor.model", Title: "Swarm conductor's model", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.worker.agent", Title: "Swarm workers' agent tool", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.worker.model", Title: "Swarm workers' model", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.strict_scope", Title: "Swarm: strict scopes", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.require_user_close", Title: "Swarm: you close workers", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.brief_kickoff_delay_ms", Title: "Swarm: kickoff delay (ms)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},

	// Voice.
	{Key: "voice.enabled", Title: "Voice dictation", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "voice.whisper_url", Title: "Whisper server", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "voice.autosend", Title: "Send dictation at once", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "voice.mic_device", Title: "Microphone", Kind: KindString, Class: ClassChange, Mode: ModeRestart},

	// Worktrees and the repository cache.
	{Key: "worktrees.yggdrasil.enabled", Title: "Yggdrasil worktrees", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "repository_cache.enabled", Title: "Fresh clones", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "repository_cache.max_age", Title: "Fetch a repository older than", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "repository_cache.keep_idle", Title: "Keep idle clones for", Description: "A duration, or forever.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},

	// Credential channels.
	{Key: "credential_channels.http_proxy.listen", Title: "Credential proxy address", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "credential_channels.ssh_agent.known_hosts", Title: "Known hosts for the SSH channel", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "credential_channels.gh.isolate_login", Title: "Keep gh's own login from agents", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},

	// Learning.
	{Key: "learning.enabled", Title: "Legato Learn", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.prompt_enabled", Title: "Learnings in launch prompts", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.transition_columns", Title: "Reflect when a card enters", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.curator_adapter", Title: "Curator's agent tool", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.curator_tier", Title: "Curator's tier", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.curator_working_dir", Title: "Curator's folder", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.packet_max_items", Title: "Learn: most items in a packet", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.packet_max_bytes", Title: "Learn: largest packet (bytes)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.reflection_timeout", Title: "Learn: reflection timeout", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.reflection_retries", Title: "Learn: reflection retries", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.prompt_max_items", Title: "Learn: most items in a prompt", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.prompt_max_bytes", Title: "Learn: largest prompt section (bytes)", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.consolidation_automatic", Title: "Learn: consolidate on its own", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.consolidation_threshold", Title: "Learn: consolidate past", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.enabled", Title: "Learn: semantic search", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.provider", Title: "Learn: embedding provider", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.model", Title: "Learn: embedding model", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.base_url", Title: "Learn: embedding address", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.api_key_env", Title: "Learn: variable with the embedding key", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "learning.embedding.timeout", Title: "Learn: embedding timeout", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.dimensions", Title: "Learn: embedding dimensions", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.batch_size", Title: "Learn: embedding batch size", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},

	// Engram.
	{Key: "engram.enabled", Title: "Engrams", Description: "auto offers engrams when the engram command answers; on does too, and reports when it does not; off hides them.", Kind: KindEnum, Enum: []string{"auto", "on", "off"}, Class: ClassChange, Mode: ModeLive},
	{Key: "engram.command", Title: "Engram command", Description: "The engram binary; engram on PATH when empty.", Kind: KindString, Class: ClassRisky, Mode: ModeLive},

	// Manticore and integrations.
	{Key: "manticore.url", Title: "Manticore address", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "manticore.api_key", Title: "Manticore key", Description: "The name of a secret holding the key.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.argus.url", Title: "Argus address", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.argus.delivery", Title: "Argus delivery", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "integrations.argus.agent", Title: "Argus's agent tool", Kind: KindString, Class: ClassChange, Mode: ModeRestart},

	// Review passes.
	{Key: "review_passes.enabled", Title: "Review passes", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.agent", Title: "Reviewer's agent tool", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.model", Title: "Reviewer's model", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.tier", Title: "Reviewer's tier", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.skill", Title: "Reviewer's skill", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.github_credential", Title: "Reviewer's GitHub credential", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "review_passes.max_passes", Title: "Most review passes", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.pass_timeout", Title: "Review pass timeout", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.max_concurrent", Title: "Most review passes at once", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.review_column", Title: "Review column", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.poll_interval", Title: "Review pass poll interval", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.quiet_time", Title: "Review pass quiet time", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.counted_bots", Title: "Review bots counted", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.required_checks", Title: "Checks a review pass waits for", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},

	// The Reeve: read when a session starts.
	{Key: "reeve.agent", Title: "The Reeve's agent tool", Description: "claude-code, codex or chimera; the default agent tool when empty.", Kind: KindString, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.model", Title: "The Reeve's model or tier", Kind: KindString, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.idle_timeout", Title: "Suspend the Reeve after", Description: "How long a session may be idle before its agent is stopped; the next message resumes it. 2h when empty.", Kind: KindDuration, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.levels.change", Title: "What a change needs", Description: "approve (a tap), signed (an enrolled device) or verified (signed, with a fingerprint or a PIN). One of the Reeve's own rules: changed only with a signature.", Kind: KindEnum, Enum: []string{"", "approve", "signed", "verified"}, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.levels.risky", Title: "What a risky change needs", Description: "auto (signed once a device is enrolled, else approve), approve, signed or verified. One of the Reeve's own rules.", Kind: KindEnum, Enum: []string{"", "auto", "approve", "signed", "verified"}, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.host_operations", Title: "Host operations", Description: "Offer the Reeve a shell, file edits and systemd services on this machine, each as a change you approve. One of the Reeve's own rules.", Kind: KindBool, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.host_timeout", Title: "Host command timeout", Description: "How long one host command may run. 10m when empty.", Kind: KindDuration, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.host_output_cap", Title: "Host command output (bytes)", Description: "How much of a host command's output the Reeve is given; the rest is kept in a file it can read. 65536 when 0.", Kind: KindInt, Class: ClassChange, Mode: ModeLive, Check: nonNegative},
	{Key: "reeve.peer", Title: "When another Reeve messages", Description: "With no session running: start one (start, the default), ask a person first (ask), or refuse. One of the Reeve's own rules.", Kind: KindEnum, Enum: []string{"", "start", "ask", "refuse"}, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.launch_args", Title: "The Reeve's launch arguments", Kind: KindStrings, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.enabled", Title: "The Reeve's voice", Description: "Lets the Reeve speak out loud on this machine's speakers. Sessions started after it is turned on are offered the speak tool; turning it off mutes the Reeve at once.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.voice.command", Title: "Voice command", Description: "Makes a line's audio: run as <command> \"<text>\" <out-file>, it writes an audio file to <out-file>. A script such as reeve-say.sh, or one for Piper.", Kind: KindString, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.credential", Title: "Voice credential", Description: "A credential Legato gives the voice command, as elevenlabs. Empty for a local voice.", Kind: KindString, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.credential_env", Title: "Voice credential variable", Description: "The variable the command finds the credential in. The credential's name in capitals with _API_KEY when empty (ELEVENLABS_API_KEY).", Kind: KindString, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.player", Title: "Voice player", Description: "Plays a line's audio file, named after it. pw-play, else ffplay, when empty.", Kind: KindString, Class: ClassRisky, Mode: ModeLive},

	// Burnlite's projects, read on each use.
	{Key: "burnlite.projects", Title: "Burnlite projects", Description: "The projects searched for import; none is every project.", Kind: KindStrings, Class: ClassChange, Mode: ModeLive, Check: func(v any) error {
		for _, p := range v.([]string) {
			if !burnliteProjectPattern.MatchString(p) {
				return fmt.Errorf("%q is not a Burnlite project key: two to six capital letters, then up to four digits, like EST", p)
			}
		}
		return nil
	}},
}

func nonNegative(v any) error {
	if v.(int) < 0 {
		return errors.New("must not be negative")
	}
	return nil
}

// secretRefPattern is a secret's name: legato://<name> in Legato's own
// store, or op://<vault>/<item>/<field> in 1Password.
var secretRefPattern = regexp.MustCompile(`^(legato://[A-Za-z0-9][A-Za-z0-9._-]{0,127}|op://[^/\s]+/[^/\s]+/.+)$`)

// fileOnly are the settings that live only in config.yaml: what is needed
// before the database opens, what unseals secrets, how the instance is
// reached, and the signing trust root.
var fileOnly = []string{
	"db", "data_dir",
	"secrets.master_key", "secrets.onepassword",
	"web.enabled", "web.port", "web.tls", "web.public_url",
	"approval_devices.android_app_digests",
}

// EntryLists lists the keys of every list of named entries: those the
// entries table keeps (EntryListDefs) and those with tables of their own.
func EntryLists() []string {
	keys := slices.Clone(tabledLists)
	for _, l := range entryListDefs {
		keys = append(keys, l.Key)
	}
	return keys
}

// SetSetting gives key a value in cfg, as Overlay does: for a secret
// setting, the secret's value once its name is resolved.
func SetSetting(cfg *Config, key string, v any) error { return setSettingValue(cfg, key, v) }

// Settings lists the registry, in its order.
func Settings() []Setting { return slices.Clone(registry) }

// FileOnlySettings lists the keys (or key prefixes) of the file-only
// settings.
func FileOnlySettings() []string { return slices.Clone(fileOnly) }

// IsFileOnly reports whether key is, or is under, a file-only setting.
func IsFileOnly(key string) bool {
	for _, f := range fileOnly {
		if key == f || strings.HasPrefix(key, f+".") {
			return true
		}
	}
	return false
}

// LookupSetting finds a registered setting by key.
func LookupSetting(key string) (Setting, bool) {
	for _, s := range registry {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// ErrUnknownSetting is a key the registry does not declare.
var ErrUnknownSetting = errors.New("not a setting that can be changed here")

// DecodeSetting reads a JSON value for s and checks it against its kind,
// its enum and its own rule.
func DecodeSetting(s Setting, raw json.RawMessage) (any, error) {
	fail := func(want string) error {
		return fmt.Errorf("%s: want %s, got %s", s.Key, want, strings.TrimSpace(string(raw)))
	}
	var v any
	switch s.Kind {
	case KindBool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, fail("true or false")
		}
		v = b
	case KindInt:
		var n int
		if json.Unmarshal(raw, &n) != nil {
			return nil, fail("a whole number")
		}
		v = n
	case KindString, KindEnum, KindDuration:
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return nil, fail("a string")
		}
		if s.Kind == KindEnum && !slices.Contains(s.Enum, str) {
			return nil, fmt.Errorf("%s: %q is not one of %s", s.Key, str, strings.Join(nonEmpty(s.Enum), ", "))
		}
		if s.Kind == KindDuration && str != "" {
			if _, err := time.ParseDuration(str); err != nil {
				return nil, fmt.Errorf("%s: %q is not a duration such as 10m", s.Key, str)
			}
		}
		v = str
	case KindStrings:
		var list []string
		if json.Unmarshal(raw, &list) != nil {
			return nil, fail("a list of strings")
		}
		if list == nil {
			list = []string{}
		}
		v = list
	default:
		return nil, fmt.Errorf("%s: unknown kind %q", s.Key, s.Kind)
	}
	if s.Secret && v != "" && !secretRefPattern.MatchString(v.(string)) {
		return nil, fmt.Errorf("%s holds a secret: give the name of one, legato://<name> in Legato's secret store or op://<vault>/<item>/<field> in 1Password, never the value", s.Key)
	}
	if s.Check != nil {
		if err := s.Check(v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func nonEmpty(values []string) []string {
	out := []string{}
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Pinned reports whether config.yaml gives key (or a part of it) a value:
// such a setting is changed only by editing the file.
func (c *Config) Pinned(key string) bool {
	if c.pins[key] {
		return true
	}
	// A value given to a key above it (a list, a scalar) pins it too.
	for parent := key; strings.Contains(parent, "."); {
		parent = parent[:strings.LastIndex(parent, ".")]
		if c.pins[parent] {
			return true
		}
	}
	return false
}

// PinnedKeys lists the registered settings config.yaml pins.
func (c *Config) PinnedKeys() []string {
	var keys []string
	for _, s := range registry {
		if c.Pinned(s.Key) {
			keys = append(keys, s.Key)
		}
	}
	return keys
}

// pinnedKeys lists every dotted path the YAML document gives a value,
// mappings walked, other values (lists, scalars) as leaves.
func pinnedKeys(data []byte) map[string]bool {
	pins := map[string]bool{}
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 {
		return pins
	}
	var walk func(prefix string, n *yaml.Node)
	walk = func(prefix string, n *yaml.Node) {
		if n.Kind != yaml.MappingNode {
			if prefix != "" && !(n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
				pins[prefix] = true
			}
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if prefix != "" {
				key = prefix + "." + key
			}
			walk(key, n.Content[i+1])
		}
	}
	walk("", doc.Content[0])
	return pins
}

// Overlay applies database values to a configuration loaded from the file:
// each to its setting unless the file pins it, then the whole configuration
// is checked with the rules Load uses. On an error cfg may be partly
// changed; load it again.
func Overlay(cfg *Config, values map[string]json.RawMessage) error {
	return OverlayAll(cfg, values, nil)
}

// OverlayAll applies the database's single values and its list entries to
// cfg, then checks the whole configuration once, so a value may name an
// entry kept in the database (default_workspace a workspace added there).
func OverlayAll(cfg *Config, values map[string]json.RawMessage, entries map[string]map[string]json.RawMessage) error {
	if err := applyValues(cfg, values); err != nil {
		return err
	}
	if err := applyEntries(cfg, entries); err != nil {
		return err
	}
	return validate(cfg)
}

func applyValues(cfg *Config, values map[string]json.RawMessage) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s, ok := LookupSetting(key)
		if !ok || cfg.Pinned(key) {
			continue
		}
		v, err := DecodeSetting(s, values[key])
		if err != nil {
			return err
		}
		if err := setSettingValue(cfg, key, v); err != nil {
			return err
		}
	}
	return nil
}

// Defaults is the configuration of an empty config.yaml.
func Defaults() *Config {
	cfg := defaults()
	applyDefaults(cfg)
	_ = validate(cfg)
	return cfg
}

// SettingValue reads key's value from cfg, pointers followed (nil is the
// zero value of the pointed type).
func SettingValue(cfg *Config, key string) (any, error) {
	field, err := settingField(cfg, key, false)
	if err != nil {
		return nil, err
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return pointerDefault(key, field), nil
		}
		return field.Elem().Interface(), nil
	}
	return field.Interface(), nil
}

// pointerDefault is the value an unset pointer setting means.
func pointerDefault(key string, field reflect.Value) any {
	switch key {
	case "notifications.approvals", "notifications.handoff", "agents.sandbox.default_mounts", "reeve.host_operations",
		"web.discovery", "credential_channels.gh.isolate_login":
		return true
	}
	return reflect.Zero(field.Type().Elem()).Interface()
}

func setSettingValue(cfg *Config, key string, v any) error {
	field, err := settingField(cfg, key, true)
	if err != nil {
		return err
	}
	value := reflect.ValueOf(v)
	if field.Kind() == reflect.Pointer {
		ptr := reflect.New(field.Type().Elem())
		ptr.Elem().Set(value.Convert(field.Type().Elem()))
		field.Set(ptr)
		return nil
	}
	if !value.Type().ConvertibleTo(field.Type()) {
		return fmt.Errorf("%s: cannot hold a %s", key, value.Type())
	}
	field.Set(value.Convert(field.Type()))
	return nil
}

// settingField finds the field of cfg a dotted key names, by yaml tags. A
// nil section on the way (such as integrations.argus) is made when alloc,
// else read as its zero value.
func settingField(cfg *Config, key string, alloc bool) (reflect.Value, error) {
	v := reflect.ValueOf(cfg).Elem()
	for _, part := range strings.Split(key, ".") {
		v = section(v, alloc)
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("%s: %w", key, ErrUnknownSetting)
		}
		found := false
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
			if tag == part {
				v = v.Field(i)
				found = true
				break
			}
		}
		if !found {
			return reflect.Value{}, fmt.Errorf("%s: %w", key, ErrUnknownSetting)
		}
	}
	return v, nil
}

// section is v, or the struct a pointer v points to: made when nil and
// alloc, else a zero value that is not part of the configuration.
func section(v reflect.Value, alloc bool) reflect.Value {
	if v.Kind() != reflect.Pointer || v.Type().Elem().Kind() != reflect.Struct {
		return v
	}
	if v.IsNil() {
		if !alloc {
			return reflect.New(v.Type().Elem()).Elem()
		}
		v.Set(reflect.New(v.Type().Elem()))
	}
	return v.Elem()
}

// PinnedSettingsAndOthers lists every dotted key this configuration's file
// gives a value: registered settings and file-only ones alike.
func (c *Config) PinnedSettingsAndOthers() []string {
	keys := make([]string, 0, len(c.pins))
	for _, s := range registry {
		if c.Pinned(s.Key) {
			keys = append(keys, s.Key)
		}
	}
	for key := range c.pins {
		if IsFileOnly(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys[len(keys)-countFileOnly(c.pins):])
	return keys
}

func countFileOnly(pins map[string]bool) int {
	n := 0
	for key := range pins {
		if IsFileOnly(key) {
			n++
		}
	}
	return n
}
