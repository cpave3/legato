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
	Key   string `json:"key"`
	Title string `json:"title"`
	// Section is where the setting lives in Settings (SettingSections),
	// and Group the headed part of that section it sits in.
	Section string `json:"section"`
	Group   string `json:"group"`
	// Help is one line on what the setting does, including what empty or
	// off means.
	Help string `json:"help,omitempty"`
	// Description is Help under its old name, kept in the API for one
	// release after help ships. Set from Help; never write it.
	Description string `json:"description,omitempty"`
	// Unit is what a number counts; a duration needs none.
	Unit SettingUnit `json:"unit,omitempty"`
	// DefaultLabel is what the setting is when unset, when that is not its
	// zero value ("4", "engram on PATH").
	DefaultLabel string       `json:"default_label,omitempty"`
	Kind         SettingKind  `json:"kind"`
	Enum         []string     `json:"enum,omitempty"`
	Class        SettingClass `json:"class"`
	Mode         SettingMode  `json:"mode"`
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
	{Key: "agents.default_tool", Title: "Default agent", Help: "The agent tool Legato starts when you name none. Unset, the workers' agent of swarms, else Claude Code.", DefaultLabel: "Workers' agent, else claude-code", Kind: KindEnum, Enum: []string{"", "claude-code", "codex", "chimera"}, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.escape_key", Title: "Key to leave an agent's terminal", Help: "Detaches you from an agent's terminal after you attach to it from the TUI, written as ctrl+].", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.sparkline_window", Title: "Activity bar covers", Help: "The stretch of time each agent's activity bar shows, in the TUI and the web UI.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.peer_awareness", Title: "Tell agents about each other", Help: "Tells an agent when others work in the same folder. Off, no notices; agents can still message each other.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.history.keep", Title: "Keep session history for", Help: "How long a past conversation stays in Session history after it was last used: a duration (36h) or days (180d); 0 keeps it until you forget it.", DefaultLabel: "180d", Kind: KindString, Class: ClassChange, Mode: ModeRestart, Check: checkAgentHistoryKeep},
	{Key: "agents.compact.auto_percent", Title: "Compact an idle agent at % full", Help: "Compacts an idle agent once its context window is this full, in percent, with its tool's own compaction. 0 never does; each tool still compacts itself near its limit.", DefaultLabel: "0 (off)", Kind: KindInt, Class: ClassChange, Mode: ModeLive, Check: percent},
	{Key: "agents.reviews", Title: "Review tour at the end", Help: "Asks agents for a review tour and a handoff packet when they finish. Off, they skip both unless you ask.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.mcp.enabled", Title: "Legato's tools", Help: "Gives every agent Legato's own tools: cards, artifacts, task notes. Off, agents have only their tool's own.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.sandbox.enabled", Title: "Sandbox agents by default", Help: "Starts every agent inside a bubblewrap sandbox unless you start it without one. Swarm and workflow agents cannot opt out.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.backend", Title: "Backend", Help: "What builds the sandbox. bwrap is the only one.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.default_mounts", Title: "Toolchains, read-only", Help: "Shows your toolchains inside the sandbox read-only, and your package and build caches read-write. Off, agents see neither.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.scratch_dir", Title: "Scratch folder", Help: "Where each sandbox's /tmp, home folder and throwaway layer are kept until its agent ends. Not your home folder or one that holds it.", DefaultLabel: "<data folder>/sandbox", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.docker.enabled", Title: "Docker for sandboxed agents", Help: "Gives each sandboxed agent a Docker socket of its own: its own containers, mounting only what its sandbox sees. Off, no Docker.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.docker.host", Title: "Docker daemon", Help: "The daemon the socket passes requests to: unix:///path or tcp://host:port without TLS.", DefaultLabel: "DOCKER_HOST, else the Docker context", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.docker.cap_add", Title: "Capabilities containers may add", Help: "Capabilities a container may add, such as SYS_PTRACE. Ones that reach the host, such as SYS_ADMIN, are refused. Empty, none.", Kind: KindStrings, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.docker.publish", Title: "Published ports bind to", Help: "loopback puts every published port on 127.0.0.1, which agents still reach; any binds what the agent asks for.", Kind: KindEnum, Enum: []string{"loopback", "any"}, Class: ClassRisky, Mode: ModeRestart},

	// Subscription usage.
	{Key: "usage.fallback_profile", Title: "Fallback profile", Help: "The usage profile for new work when no usage rule matches. Empty keeps the usual defaults.", Kind: KindString, Class: ClassChange, Mode: ModeLive},
	{Key: "usage.claude.enabled", Title: "Claude", Help: "Reads how much of your Claude subscription's 5-hour and weekly limits you have used, with Claude Code's login. Off, Legato never asks.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "usage.codex.enabled", Title: "Codex", Help: "Reads how much of your ChatGPT account's Codex limits you have used, through the codex command or its session logs. Off, Legato never asks.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "usage.opencode.enabled", Title: "OpenCode Go", Help: "Reads how much of your OpenCode Go subscription's 5-hour, weekly and monthly limits you have used, with the API key named below. Off (the default), Legato never asks. Applies at restart.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "usage.opencode.api_key", Title: "OpenCode Go API key", Help: "The name of a secret holding your OpenCode Go API key, such as legato://opencode-go; store it with `legato secret add`. Replacing the key's value in the secret store applies when Legato restarts, not before.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "usage.poll_interval", Title: "Read usage every", Help: "How often Legato reads each subscription's usage. At least 2m.", DefaultLabel: "5m", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart, Check: checkUsagePollInterval},

	// Notifications.
	{Key: "notifications.usage", Title: "Usage profile switches", Help: "Notifies you when the active usage profile changes.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "notifications.ntfy.url", Title: "ntfy server", Help: "The ntfy server that relays notifications to your phone.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.ntfy.topic", Title: "Topic", Help: "The topic to publish to; pick one nobody could guess. Empty, no phone notifications are sent.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.os.enabled", Title: "On this computer's desktop", Help: "Shows notifications on the desktop of the machine Legato runs on, through notify-send or osascript.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.approvals", Title: "Approvals", Help: "Notifies you of each new approval request; tapping it opens the request.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.handoff", Title: "Handoffs", Help: "On a main instance, notifies you when an away task fails or is lost, or a worker stops answering.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},

	// Look and feel.
	{Key: "theme", Title: "TUI theme", Help: "The TUI's colours. Only default exists for now.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "icons", Title: "TUI icons", Help: "The glyphs on the TUI's cards: unicode, or nerdfonts when your terminal font is a Nerd Font.", DefaultLabel: "unicode", Kind: KindEnum, Enum: []string{"unicode", "nerdfonts"}, Class: ClassChange, Mode: ModeRestart},
	{Key: "editor", Title: "TUI editor", Help: "The command the TUI opens to edit a task's description.", DefaultLabel: "$VISUAL, then $EDITOR, then vi", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "web.tint", Title: "Tint", Help: "A colour that tells this Legato from others in the web UI. Empty, none.", Kind: KindEnum, Enum: []string{"", "red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}, Class: ClassChange, Mode: ModeLive},

	// Updates.
	{Key: "updates.mode", Title: "Install updates", Help: "auto installs a new release once no agent is mid-turn, and restarts; ask offers it; off never looks.", DefaultLabel: "ask", Kind: KindEnum, Enum: []string{"auto", "ask", "off"}, Class: ClassRisky, Mode: ModeLive},

	{Key: "notifications.ntfy.token", Title: "Token", Help: "The name of a secret holding the token of a protected topic. Empty for a public topic.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "keybindings.vim_mode", Title: "Vim keys", Help: "Not used yet: the board always takes vim keys.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "web.rail.actions", Title: "Actions", Help: "Things agents ask you to do that only you can: a command, a test in a browser, a merge.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.questions", Title: "Questions", Help: "Questions agents asked you in plain words.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.done", Title: "Finished work", Help: "Work agents finished for you to look at.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.other", Title: "Anything else", Help: "Anything else agents post for you.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.approvals", Title: "Approvals", Help: "Requests that hold an agent mid-turn, and paired instances' change sets.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.agents", Title: "Agents", Help: "Agents waiting for you or stopped on an error.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.workflows", Title: "Workflows", Help: "Workflow gates, failures, stuck review loops and triggers.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.plans", Title: "Plans", Help: "Plans waiting for your decision.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.review", Title: "Ready for review", Help: "Review tours ready and handoffs done. Off unless set.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.merge", Title: "Merge blockers", Help: "Pull requests that cannot merge, and Argus's cases. Off unless set.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.rail.snooze", Title: "After you close it on a phone", Help: "How long the needs-you sheet stays closed before it opens by itself again. A new approval, question, plan or gate opens it sooner.", DefaultLabel: "30m", Kind: KindDuration, Class: ClassChange, Mode: ModeLive, Check: positiveDuration},
	{Key: "web.rail.legato", Title: "Legato", Help: "Updates, restarts, settings, devices, handoff, integrations, engrams and interrupted agents.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "web.discovery", Title: "Announce on the network", Help: "Announces the web server over mDNS so the phone app finds it. Off, you type its address.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.mcp.resource_domains", Title: "Hosts agents' pages may load from", Help: "Plain host names that pages agents show may load scripts, styles, fonts and images from. Empty, none.", Kind: KindStrings, Class: ClassRisky, Mode: ModeRestart},
	{Key: "groups.defaults", Title: "Agent groups", Help: "Group names always offered when you set an agent's group, beside those agents already have.", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "default_workspace", Title: "New tasks go to", Help: "The workspace a new task goes in when you pick none. Empty, no workspace.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "default_import_workspace", Title: "Imported tasks go to", Help: "The workspace an imported ticket goes in when you pick none.", DefaultLabel: "Same as new tasks", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "ssh_agent_socket", Title: "SSH agent socket", Help: "Sets SSH_AUTH_SOCK for Legato and what it starts. Empty, the one Legato started with, else 1Password's agent.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},

	// Jira.
	{Key: "jira.base_url", Title: "Address", Help: "Your Jira site, such as https://example.atlassian.net. Empty, Jira is not used.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.email", Title: "Email", Help: "The email address of your Atlassian account.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.api_token", Title: "API token", Help: "The name of a secret holding an Atlassian API token.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "jira.project_keys", Title: "Projects", Help: "The projects the import search looks in. Empty searches every project.", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.jql_filter", Title: "Filter", Help: "JQL added to every sync's query; a tracked ticket that does not match is not refreshed.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.sync_interval_seconds", Title: "Sync every", Help: "How often Legato pulls the tickets it tracks.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},
	{Key: "jira.status_sync_enabled", Title: "Move issues with their cards", Help: "Moves cards when their Jira status changes, and the issue when you move a card. Off, statuses never change.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "jira.attachment_max_size_bytes", Title: "Largest attachment", Help: "The largest image attachment Legato downloads from a ticket; it downloads only images.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},

	// Burnlite.
	{Key: "burnlite.url", Title: "Address", Help: "Your Burnlite, such as https://burnlite.example.com, used when you connect without naming one.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.sync_interval_seconds", Title: "Sync every", Help: "How often Legato pulls the tasks it tracks.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},
	{Key: "burnlite.status_sync_enabled", Title: "Move cards with Burnlite", Help: "Moves a card when its task changes state in Burnlite. Off, cards stay where you put them.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.ready", Title: "Ready column", Help: "The column of an open task that nobody has claimed and nothing blocks.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.blocked", Title: "Blocked column", Help: "The column of an open task with an open blocker.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.claimed", Title: "Claimed column", Help: "The column of an open task someone has claimed.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "burnlite.columns.closed", Title: "Closed column", Help: "The column of a closed task, completed or not planned.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},

	// GitHub.
	{Key: "github.auth", Title: "Sign in with", Help: "auto uses the token below if set, else gh auth token; gh only gh; token only the token below.", Kind: KindEnum, Enum: []string{"auto", "gh", "token"}, Class: ClassRisky, Mode: ModeRestart},
	{Key: "github.token", Title: "Token", Help: "The name of a secret holding a GitHub token.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "github.poll_interval_seconds", Title: "Check pull requests every", Help: "How often Legato looks for a pull request on a task's branch that has none yet.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},
	{Key: "github.app_logins", Title: "Legato's GitHub App logins", Help: "Bot logins of Legato's own GitHub Apps, such as my-reviewer[bot], whose reviews never start a workflow.", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "github.resolved_poll_interval_seconds", Title: "Check known pull requests every", Help: "How often Legato refreshes pull requests it already knows.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},

	// Swarms.
	{Key: "swarm.enabled", Title: "Swarms", Help: "Turns on swarms: legato swarm, the swarm endpoints, and Start swarm on cards and in the TUI.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.max_concurrent_agents", Title: "Workers at once", Help: "Live workers in one swarm. More wait for a free slot.", DefaultLabel: "4", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.max_subtasks_per_plan", Title: "Parts in a plan", Help: "A plan with more parts, in all or in one step, is refused.", DefaultLabel: "10", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.max_steps_per_plan", Title: "Steps in a plan", Help: "The most ordered steps a plan may have.", DefaultLabel: "10", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.conductor.agent", Title: "Conductor", Help: "The agent tool that plans the work and dispatches the workers.", DefaultLabel: "Workers' agent", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.conductor.model", Title: "Conductor's model", Help: "A model or tier for new conductors, added to the tool's launch flags.", DefaultLabel: "The tool's default", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.worker.agent", Title: "Workers", Help: "The agent tool a worker runs when its part names none. Also the default agent while that is unset.", DefaultLabel: "claude-code", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.worker.model", Title: "Workers' model", Help: "A model or tier for workers that name no tier.", DefaultLabel: "The tool's default", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.strict_scope", Title: "Strict file scopes", Help: "Refuses to dispatch a worker whose files overlap another's. Off, an overlap only warns the conductor.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "swarm.brief_kickoff_delay_ms", Title: "Kickoff delay", Help: "How long Legato waits after starting a tool before it types \"read your brief\". Raise it for a slow tool.", DefaultLabel: "250", Kind: KindInt, Class: ClassChange, Mode: ModeRestart, Check: nonNegative},

	// Voice.
	{Key: "voice.enabled", Title: "Dictation", Help: "Lets you dictate messages to agents. Stays off without a Whisper server.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "voice.whisper_url", Title: "Whisper server", Help: "The Wyoming address of a Whisper server, such as tcp://192.168.1.220:10300; it can be Home Assistant's.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "voice.autosend", Title: "Send dictation at once", Help: "Sends what you said as a message. Off, it is typed into the agent's terminal for you to finish.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "voice.mic_device", Title: "Microphone", Help: "The ALSA device to record from, such as hw:1,0.", DefaultLabel: "default", Kind: KindString, Class: ClassChange, Mode: ModeRestart},

	// Worktrees and the repository cache.
	{Key: "worktrees.yggdrasil.enabled", Title: "Make worktrees with Yggdrasil", Help: "Lets Legato make and remove a task's Git worktree with yg, and the workflow steps that do.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "repository_cache.enabled", Title: "Offer fresh clones", Help: "Offers Clone beside Folder when you start an agent: a fresh clone on a branch of its own. Jobs can clone either way.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "repository_cache.max_age", Title: "Refresh the cached copy after", Help: "How long Legato trusts its copy of a repository before a job fetches it again; 0s fetches every time.", DefaultLabel: "5m", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "repository_cache.keep_idle", Title: "Keep idle clones for", Help: "How long a clone no agent works in is kept: a duration (36h) or days (7d); 0 keeps clones until you remove them.", DefaultLabel: "7d", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},

	// Credential channels.
	{Key: "credential_channels.http_proxy.listen", Title: "Credential proxy address", Help: "The loopback address of Legato's proxy for API keys. Port 0 picks a free one and keeps it across restarts.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "credential_channels.ssh_agent.known_hosts", Title: "Known hosts for SSH", Help: "The file that names SSH hosts by their keys, so credential rules can name hosts.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "credential_channels.gh.isolate_login", Title: "Keep gh's own login from agents", Help: "With a github credential, an agent's gh gets Legato's tokens or none. Off, it falls back to your own login.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},

	// Learning.
	{Key: "learning.enabled", Title: "Learn (deprecated)", Help: "Does nothing now. Automatic Learn is the learn workflow: add it and switch its trigger on and off on the Workflows tab.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.prompt_enabled", Title: "Add learnings to launch prompts", Help: "Adds accepted lessons to the prompt of agents Legato starts.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.transition_columns", Title: "Reflect when a card moves to (deprecated)", Help: "Does nothing now. The learn workflow's trigger names its column.", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.curator_adapter", Title: "Curator", Help: "The agent tool the curator of legato learn curate and consolidate runs. The learn workflow names its own.", DefaultLabel: "Same as the default agent", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.curator_tier", Title: "Curator's tier", Help: "A tier of the curator's tool to run it with.", DefaultLabel: "The tool's default", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.curator_working_dir", Title: "Curator's folder", Help: "An absolute folder the curator always works in.", DefaultLabel: "The task's checkout", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.packet_max_items", Title: "Items in a packet", Help: "The most items in what the curator is given.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.packet_max_bytes", Title: "Packet size", Help: "The most bytes in what the curator is given.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.reflection_timeout", Title: "Reflection timeout", Help: "Not used yet. It must still be a positive duration.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.reflection_retries", Title: "Reflection retries", Help: "Not used yet. It must still be positive.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.prompt_max_items", Title: "Lessons in a prompt", Help: "The most lessons added to an agent's prompt.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.prompt_max_bytes", Title: "Prompt section size", Help: "The most bytes of lessons added to an agent's prompt.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.consolidation_automatic", Title: "Consolidate on its own", Help: "Not used yet: run consolidation by hand.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.consolidation_threshold", Title: "Consolidate past", Help: "Not used yet. It must still be positive.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.enabled", Title: "Semantic search", Help: "Finds lessons by meaning as well as by words, through an embedding API.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.provider", Title: "Provider", Help: "The kind of API. openai-compatible is the only one.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.model", Title: "Model", Help: "The embedding model. Needed while semantic search is on.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.base_url", Title: "Address", Help: "The embedding API's address.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.api_key_env", Title: "Key variable", Help: "The environment variable that holds the API key. A local server may need none.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "learning.embedding.timeout", Title: "Request timeout", Help: "How long one embedding request may take.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.dimensions", Title: "Dimensions", Help: "The vector size to ask for. 0 takes what the model gives.", DefaultLabel: "What the model gives", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "learning.embedding.batch_size", Title: "Batch size", Help: "Not used yet. It must still be positive.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},

	// Engram.
	{Key: "engram.enabled", Title: "Offer engrams", Help: "auto offers engrams when the engram command answers; on does too and logs when it does not; off hides them.", Kind: KindEnum, Enum: []string{"auto", "on", "off"}, Class: ClassChange, Mode: ModeLive},
	{Key: "skills.dir", Title: "Skills folder", Help: "The folder of skills Legato gives agents, one folder per skill, each with a SKILL.md.", DefaultLabel: "~/.legato/skills", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "engram.command", Title: "Command", Help: "The engram binary.", DefaultLabel: "engram on PATH", Kind: KindString, Class: ClassRisky, Mode: ModeLive},

	// Manticore and integrations.
	{Key: "manticore.url", Title: "Address", Help: "The Manticore instance's address. Empty, Claude Code sessions report no usage.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "manticore.api_key", Title: "Key", Help: "The name of a secret holding the key of the Manticore client usage counts against.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.mqtt.enabled", Title: "Publish", Help: "Publishes read-only Home Assistant entities to an MQTT broker. Off with theme control off, Legato makes no MQTT connection.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.mqtt.theme_control", Title: "Let Home Assistant set the theme", Help: "Lets broker clients allowed to write the theme/set topic change this Legato's theme; needs no Legato token.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.mqtt.broker", Title: "Broker", Help: "An mqtt:// or mqtts:// address, without credentials. Needed when either switch is on.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.mqtt.credential", Title: "Login", Help: "The name of a host-only mqtt userpass credential. Empty, anonymous access.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.mqtt.discovery_prefix", Title: "Discovery prefix", Help: "Home Assistant's discovery topic root.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.mqtt.base_topic", Title: "Base topic", Help: "Where state is published; each instance needs its own.", DefaultLabel: "legato/<instance ID>", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.mqtt.device_name", Title: "Device name", Help: "The device's name in Home Assistant; renaming keeps its identity.", DefaultLabel: "Legato <hostname>", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "integrations.mqtt.include_reeve", Title: "Count the Reeve", Help: "Counts the Reeve among the agents. Attention and approvals are always counted for the whole instance.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "integrations.mqtt.attention_details", Title: "Attention details", Help: "Publishes up to 20 generated labels of Attention items, never their titles. Retained data can outlive Legato.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.slack.enabled", Title: "Connect", Help: "Keeps the Slack bot connected over Socket Mode, so workflows can start on its messages and post as it.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.slack.app_token", Title: "App-level token", Help: "The name of a secret holding the app's xapp- token, with connections:write.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.slack.bot_token", Title: "Bot token", Help: "The name of a secret holding the app's xoxb- bot token.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.slack.default_channel", Title: "Default channel", Help: "The channel ID a workflow's message goes to when its step names none.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "integrations.discord.enabled", Title: "Connect", Help: "Keeps the Discord bot connected to the Gateway, so workflows can start on its messages and post as it.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.discord.token", Title: "Bot token", Help: "The name of a secret holding the bot's token.", Kind: KindString, Secret: true, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.discord.intents", Title: "Gateway intents", Help: "Of guilds, guild_messages, direct_messages and message_content. Without message_content, a server message's text arrives only when it mentions the bot.", Kind: KindStrings, Class: ClassRisky, Mode: ModeRestart, Check: checkDiscordIntents},
	{Key: "integrations.discord.default_channel", Title: "Default channel", Help: "The channel ID a workflow's message goes to when its step names none.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "integrations.argus.url", Title: "Address", Help: "Argus's address: https, or http to this machine. Empty, Argus is not used.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "integrations.argus.delivery", Title: "Hand cases over", Help: "auto hands each case to an agent at once; ask waits for your approval.", DefaultLabel: "auto", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "integrations.argus.agent", Title: "Agent for its briefs", Help: "The agent tool an agent started for a case runs.", DefaultLabel: "claude-code", Kind: KindString, Class: ClassChange, Mode: ModeRestart},

	// Review passes.
	{Key: "review_passes.enabled", Title: "Review passes", Help: "Reviews make-a-change handoffs on this worker in passes, and tells main instances it does. Needs Legato's tools.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.agent", Title: "Reviewer", Help: "The reviewer's agent tool. Needed while review passes are on; pick a different model from the author's.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.model", Title: "Reviewer's model", Help: "The reviewer's model. Set this or the tier, not both.", DefaultLabel: "The tool's default", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.tier", Title: "Reviewer's tier", Help: "A tier of the reviewer's tool. Set this or the model, not both.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.reviewer.skill", Title: "Reviewer's skill", Help: "The review skill the reviewer is told to use, such as 3-tier-review.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.github_credential", Title: "Reviewer's GitHub credential", Help: "The credential of the reviewer's GitHub App, on the github-reviewer profile, that posts reviews. Needed while on.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "review_passes.max_passes", Title: "Passes before asking", Help: "Passes before a review is stuck and the main instance is asked.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.pass_timeout", Title: "Time for one pass", Help: "How long a reviewer has for one pass.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.max_reentries", Title: "Re-entries after approval", Help: "Times a stewarded pull request may go back to review after it was approved. A workflow may set fewer, never more.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.max_concurrent", Title: "Reviewers at once", Help: "Reviewers that run at once on this worker; more wait.", Kind: KindInt, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.review_column", Title: "Review column", Help: "The column a card is in while it is reviewed.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.poll_interval", Title: "Check for new reviews every", Help: "How often pull requests are checked for new comments.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.quiet_time", Title: "Wait after the last review", Help: "How long new feedback waits for more before the author is told.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.counted_bots", Title: "Bots that count as reviewers", Help: "Bot logins whose comments count as feedback, as the repository's people do.", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},
	{Key: "review_passes.required_checks", Title: "Checks to wait for", Help: "The checks that must pass. Empty, the branch protection's required checks.", Kind: KindStrings, Class: ClassChange, Mode: ModeRestart},

	// The Reeve: read when a session starts.
	{Key: "reeve.name", Title: "Name", Help: "What Legato calls the Reeve on this instance, everywhere it shows and in its own prompt.", DefaultLabel: "the Reeve", Kind: KindString, Class: ClassChange, Mode: ModeLive, Check: func(v any) error { return CheckReeveName(v.(string)) }},
	{Key: "reeve.agent", Title: "Agent", Help: "The agent tool a Reeve session runs.", DefaultLabel: "Same as the default agent", Kind: KindString, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.model", Title: "Model or tier", Help: "The model or tier a Reeve session runs.", DefaultLabel: "The tool's default", Kind: KindString, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.idle_timeout", Title: "Suspend after", Help: "How long a session may be idle before its agent is stopped; the next message resumes it.", DefaultLabel: "2h", Kind: KindDuration, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.levels.change", Title: "Approval for a change", Help: "auto runs at once; approve takes a tap; signed an enrolled device; verified a fingerprint or PIN too. Loosening it needs a signature.", DefaultLabel: "approve", Kind: KindEnum, Enum: []string{"", "auto", "approve", "signed", "verified"}, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.levels.risky", Title: "Approval for a risky change", Help: "Restarts, stopped agents, what runs here. default is signed once a device is enrolled, else approve; auto runs them at once.", DefaultLabel: "default", Kind: KindEnum, Enum: []string{"", "default", "auto", "approve", "signed", "verified"}, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.host_operations", Title: "Host operations", Help: "Offers the Reeve a shell, file edits and systemd services on this machine, each as a change you approve.", Kind: KindBool, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.host_operations_auto", Title: "Run host operations without asking", Help: "Runs the Reeve's commands as your user at once, audited; as root they still need a verified approval. Turning it on needs a signature.", Kind: KindBool, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.host_timeout", Title: "Command timeout", Help: "How long one host command may run.", DefaultLabel: "10m", Kind: KindDuration, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.host_output_cap", Title: "Output the Reeve reads", Help: "The most of a command's output the Reeve is given. Longer output, or more than 40 lines, is cut to its first and last 20 lines, and the whole of it is kept in a file the Reeve can read.", DefaultLabel: "65536", Kind: KindInt, Class: ClassChange, Mode: ModeLive, Check: nonNegative},
	{Key: "reeve.peer", Title: "When another Reeve messages", Help: "With no session running: start one, ask you first in Attention, or refuse. Changed only with a signature.", DefaultLabel: "start", Kind: KindEnum, Enum: []string{"", "start", "ask", "refuse"}, Class: ClassRisky, Mode: ModeLive, OwnRule: true},
	{Key: "reeve.launch_args", Title: "Launch arguments", Help: "Arguments added to the agent tool's launch command for a Reeve session.", Kind: KindStrings, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.enabled", Title: "Speak out loud", Help: "Lets the Reeve speak on this machine's speakers. New sessions get the speak tool; turning it off mutes at once.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.voice.command", Title: "Voice command", Help: "Makes a line's audio, run as <command> \"<text>\" <out-file>: a script such as reeve-say.sh, or Piper.", Kind: KindString, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.credential", Title: "Voice credential", Help: "A credential Legato gives the voice command, as elevenlabs. Empty for a local voice.", Kind: KindString, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.credential_env", Title: "Credential variable", Help: "The variable the command finds the credential in.", DefaultLabel: "<NAME>_API_KEY", Kind: KindString, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.voice.pause", Title: "Pause that ends a spoken turn", Help: "How long you stay quiet before what you said is sent, from 0.3s to 2s.", DefaultLabel: "1s", Kind: KindDuration, Class: ClassChange, Mode: ModeLive, Check: voicePause},
	{Key: "reeve.voice.follow_up", Title: "Listen for a reply for", Help: "How long the voice listens for your reply after the Reeve has spoken; 0s for a single turn.", DefaultLabel: "6s", Kind: KindDuration, Class: ClassChange, Mode: ModeLive, Check: notNegativeDuration},
	{Key: "reeve.voice.partials", Title: "Words as you speak", Help: "Shows the words so far by transcribing about once a second. Turn it off when Whisper runs on a CPU or is shared.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.voice.quiet_hours", Title: "Quiet hours", Help: "When no line plays on this machine's speakers, as 22:00-07:00 local time; the Reeve writes instead. Empty, none.", Kind: KindString, Class: ClassChange, Mode: ModeLive, Check: func(v any) error {
		if s := v.(string); s != "" {
			_, _, err := ParseQuietHours(s)
			return err
		}
		return nil
	}},
	{Key: "reeve.voice.player", Title: "Voice player", Help: "Plays a line's audio file, named after it.", DefaultLabel: "pw-play, else ffplay", Kind: KindString, Class: ClassRisky, Mode: ModeLive},
	{Key: "reeve.watch.settle", Title: "Tell the Reeve once an agent is idle for", Help: "How long a watched agent must stay idle, or busy only in the background, before the Reeve is told.", DefaultLabel: "30s", Kind: KindDuration, Class: ClassChange, Mode: ModeLive, Check: notNegativeDuration},
	{Key: "reeve.watch.started", Title: "Watch the agents the Reeve starts", Help: "Watches every agent the Reeve starts or messages, so it hears when one waits, finishes or exits.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},
	{Key: "reeve.watch.wake", Title: "Wake the Reeve for its agents", Help: "Starts or resumes a session when a watched agent needs the Reeve. Off, the news waits; on costs tokens while you are away.", Kind: KindBool, Class: ClassChange, Mode: ModeLive},

	// Burnlite's projects, read on each use.
	{Key: "burnlite.projects", Title: "Projects", Help: "The project keys the import search looks in. Empty, the projects saved here, else every project.", Kind: KindStrings, Class: ClassChange, Mode: ModeLive, Check: func(v any) error {
		for _, p := range v.([]string) {
			if !burnliteProjectPattern.MatchString(p) {
				return fmt.Errorf("%q is not a Burnlite project key: two to six capital letters, then up to four digits, like EST", p)
			}
		}
		return nil
	}},
}

// voicePause keeps reeve.voice.pause to 0.3–2s.
func voicePause(v any) error {
	if s := v.(string); s != "" {
		if d, _ := time.ParseDuration(s); d < MinVoicePause || d > MaxVoicePause {
			return errors.New("must be from 0.3s to 2s")
		}
	}
	return nil
}

func positiveDuration(v any) error {
	if s := v.(string); s != "" {
		if d, _ := time.ParseDuration(s); d <= 0 {
			return errors.New("must be longer than 0s")
		}
	}
	return nil
}

func notNegativeDuration(v any) error {
	if s := v.(string); s != "" {
		if d, _ := time.ParseDuration(s); d < 0 {
			return errors.New("must not be negative")
		}
	}
	return nil
}

func percent(v any) error {
	if n := v.(int); n < 0 || n > 100 {
		return errors.New("must be a percentage from 0 to 100")
	}
	return nil
}

func checkDiscordIntents(v any) error {
	for _, name := range v.([]string) {
		if !slices.Contains(DiscordIntentNames, name) {
			return fmt.Errorf("%q is not an intent: want %s", name, strings.Join(DiscordIntentNames, ", "))
		}
	}
	return nil
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
// reached, and the signing trust root. agents.repository_resources is
// file-only because it names resources of the file's resources block, which
// load checks; a resource saved in the database is not there to check.
var fileOnly = []string{
	"db", "data_dir",
	"secrets.master_key", "secrets.onepassword",
	"web.enabled", "web.port", "web.tls", "web.public_url",
	"approval_devices.android_app_digests",
	"agents.repository_resources",
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
	if group, ok := strings.CutPrefix(key, "web.rail."); ok {
		return RailGroupDefault(group)
	}
	switch key {
	case "notifications.approvals", "notifications.handoff", "notifications.usage", "agents.sandbox.default_mounts", "reeve.host_operations", "reeve.voice.partials", "reeve.watch.started",
		"web.discovery", "credential_channels.gh.isolate_login", "usage.claude.enabled", "usage.codex.enabled":
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
