## Shared repository presets

Named repository presets support workflow/web agent input and establish task GitHub context without preventing overrides:

```yaml
repositories:
  - name: Legato
    repo: cpave3/legato
    primary_dir: /home/cameron/Projects/legato
    base_branch: main
    jira_projects: [LEG]
    workspaces: [Legato]
```

Legacy `workflows.repositories` is loaded only when the shared top-level catalog is absent.

- `name`: dropdown label.
- `repo`: GitHub `owner/repository` value.
- `primary_dir`: absolute path to the primary local checkout.
- `base_branch`: optional default branch.

For workflow launches, the preset control appears only when the definition declares `repo`, `primary_dir`, or `base_branch`. Selecting a preset prefills only declared matching inputs; undeclared values are not submitted. The ephemeral-agent and task-agent web dialogs use the same list and copy the selected preset's `primary_dir` into their editable working-directory field. Editing that field switches the selector to **Custom** and the manual value is submitted.

## Config

- Config file location: `$LEGATO_CONFIG` > `$XDG_CONFIG_HOME/legato/config.yaml` > `~/.config/legato/config.yaml`
- Missing config file returns defaults (no error) — app starts without config for initial setup
- Env vars expanded before YAML parsing: `${LEGATO_JIRA_TOKEN}` works in config values
- `icons` field: `"unicode"` (default) or `"nerdfonts"` for Nerd Font glyphs on cards
- `editor` field: optional editor override for description editing (used by `config.ResolveEditor`)
- `db.driver` selects the persistence backend. It defaults to `sqlite`, which is currently the only supported runtime backend; unknown drivers fail during startup. `db.path` keeps its existing precedence and default (`$XDG_DATA_HOME/legato/legato.db` or `~/.local/share/legato/legato.db`). All CLI, TUI, and server entry points resolve this configuration through one store opener.
- `data_dir` optionally selects the filesystem root for auth tokens, certificates, Jira attachment cache files (`attachments/`), and workflow artifacts (`artifacts/`). When omitted it defaults to the parent of the resolved SQLite path, preserving existing file locations while keeping filesystem storage independent from future non-file database locators.
- `agents.tmux_options` field: map of tmux option key→value pairs applied to each spawned agent session via `tmux set-option` (e.g., `mouse: "on"`, `history-limit: "50000"`)
- `agents.sparkline_window` field: Go duration string (default `"10m"`). Controls the time window aggregated into the per-agent sparkline shown in the TUI agents sidebar and on `/api/agents` (web). Invalid values silently fall back to 10m. Threaded into the TUI via `App.SetSparklineWindow(d, buckets)` and into the web server via `Server.SetSparklineWindow(d, buckets)` (called from `cmd/legato/main.go` via `resolveSparklineWindow`). The bucket count is currently fixed at 10
- `macros` field: top-level list of `{name, keys}` objects defining named send-keys payloads. `name` is the display label; `keys` is the raw text sent to a worker's tmux pane (trailing `\n` becomes a real Enter). Exposed in the TUI agents view via the `m` keybinding (opens the macro picker overlay), in the web `PromptBar` as a Macros dropdown, and over HTTP via `GET /api/macros`. No env-var expansion or placeholder substitution — strings are sent verbatim
- `workspaces` field: list of `{name, color}` objects defining workspaces. Seeded to DB on startup via `service.SeedWorkspaces`. Color is hex string (e.g. `"#4A9EEF"`). Names must be non-empty and unique case-insensitively.
- `default_workspace` and `default_import_workspace` optionally name entries from `workspaces` (matching is case-insensitive). Example:
  ```yaml
  workspaces:
    - name: Work
      color: "#4A9EEF"
    - name: Personal
      color: "#7BC47F"
  default_workspace: Personal
  default_import_workspace: Work
  ```
  `default_workspace` prefills new local tasks in the TUI and web UI and applies to `legato task create` when `--workspace` is omitted. `default_import_workspace` prefills provider-import selectors and applies to import API requests that omit `workspace_id`; when it is absent, imports fall back to `default_workspace`. Explicit selections always win, including **No workspace**, JSON `workspace_id: null`, and CLI `--workspace none` or `--workspace unassigned`. If `default_workspace` is absent, TUI local creation retains its existing active-workspace prefill while web and CLI creation remain unassigned. Unknown or ambiguous names fail config loading instead of silently routing a task. Defaults affect only newly created/imported tasks: they do not backfill existing tasks, alter workflow-selected placement, or move an already tracked ticket when it is imported again.
- `web.enabled` field: bool, auto-start web server alongside TUI. On startup, probes the port — if free, starts server in background goroutine sharing the same services. If port is in use (another instance), it skips silently. Server shuts down cleanly when TUI exits. Status bar shows `Web :<port>` indicator. `web.port` field: string (default `"3080"`). HTTP is the default. Set `web.tls.enabled: true` to enable HTTPS with an auto-generated CA and server certificate at `<resolved data_dir>/certs/` (`$XDG_DATA_HOME/legato/certs/` or `~/.local/share/legato/certs/` when `data_dir` is omitted). Setting both `web.tls.cert` and `web.tls.key` also enables HTTPS and uses those files. Auto-generated server certificates include SANs for localhost, loopback, all current local network IPs, and optional `web.tls.hostname`. They regenerate when local IPs or the hostname change while retaining the CA.
- `github.auth`: `auto` (default), `gh`, or `token`. `auto` prefers `github.token` and otherwise retrieves the active GitHub CLI credential with exactly `gh auth token`; `gh` requires CLI authentication and `token` requires explicit configuration. Credentials remain memory-only. GitHub data access uses a GET-only HTTP transport—`gh` is not used for API or mutation commands. `github.poll_interval_seconds` controls unresolved discovery (default 600s); `github.resolved_poll_interval_seconds` controls linked refresh (default 600s). Manual `r` bypasses intervals.
- `learning` block: opt-in learning workflow settings. `learning.enabled` defaults to `false`; configuration alone does not start learning or alter task behavior. Example:
  ```yaml
  learning:
    enabled: false          # automatic reflection/curation
    prompt_enabled: false  # global prompt-composition kill switch
    transition_columns: [Review]
    curator_adapter: chimera
    curator_tier: careful
    # Optional global override; omit to use each task's checkout/worktree.
    # curator_working_dir: /home/user/Projects/legato
    packet_max_items: 100
    packet_max_bytes: 262144
    reflection_timeout: 5m
    reflection_retries: 3
    prompt_max_items: 20
    prompt_max_bytes: 16384
    consolidation_automatic: false
    consolidation_threshold: 50
  ```
  Limits, retry counts, the consolidation threshold, and the reflection timeout must be positive. When enabled, transition columns must name configured board columns. `learning.curator_adapter` overrides `swarm.worker.agent`; the resolved adapter must exist under `adapters`, and a configured curator tier must exist for it. Task curation uses the target task's checkout/worktree by default; `curator_working_dir`, when set, is a global absolute-path override. `enabled` gates automatic column-entry triggers. `prompt_enabled` independently gates launch-time prompt composition and defaults off. Neither setting disables the ledger or manual CLI/API/web access. `reflection_timeout`, `reflection_retries`, `consolidation_automatic`, and `consolidation_threshold` are reserved settings: they are parsed and validated but currently have no operational effect. Consolidation remains manual. See [Legato Learn](learning.md) for exact behavior, privacy, and prompt eligibility.
- `swarm` block: swarm-orchestration settings.
  - `swarm.worker.agent` (string, optional): adapter used when a worker plan entry omits `agent`. When unset, Legato uses the registered default adapter.
  - `swarm.worker.model` (string, optional): direct model for workers that use the worker agent and do not specify a tier. An explicit different plan agent does not inherit this model.
  - `swarm.conductor.agent` (string, optional): adapter for newly spawned conductors. Falls back to `swarm.worker.agent`, then the registered default.
  - `swarm.conductor.model` (string, optional): direct model for newly spawned conductors. Ad-hoc promotion does not change the running process model.
  - `swarm.max_concurrent_agents` (int, default 4): cap on simultaneously-live workers per swarm.
  - `swarm.max_subtasks_per_plan` (int, default 10): plan-size cap; `propose-plan` rejects oversized plans.
  - `swarm.strict_scope` (bool, default false): when true, scope-overlap conflicts hard-block dispatch; when false, conflicts are advisory and surfaced as `scope_warning` events to the conductor.
  - `swarm.brief_kickoff_delay_ms` (int, default 250): pause between auto-launch and the "read your brief" send-keys for adapters that need a separate kickoff (claude). Bump if your adapter takes longer to render its prompt.
  - `swarm.prompts` (map): per-role per-adapter overrides for the system prompt content. Outer key = role label (`conductor`, free-form worker labels like `backend`); inner key = adapter name (`claude-code`, `chimera`, `codex`). Built-in defaults under `internal/engine/hooks/prompts/conductor.md` and `worker.md` apply when unset.
  - `swarm.require_user_close` (bool, default false): reserved (no-op currently).
- `adapters` block: per-adapter launch settings, keyed by adapter name.
  - `adapters.<name>.launch_args` (string list): extra CLI flags appended to the adapter's auto-launch command. Shell-quoted automatically. Examples: `["--dangerously-skip-permissions"]` for claude, `["--sandbox"]` for chimera, `["--model", "gpt-4o"]` for codex.
  - `adapters.<name>.modes` (map): role label → adapter mode name. Adapter-specific feature; only the chimera adapter currently consumes it. Resolution: exact role match first, then non-conductor falls back to `worker`. Legato ships no default mode files — users opt in by creating `~/.chimera/modes/<name>.md` themselves and mapping roles to them. When unset, no `--mode` flag is injected; chimera uses its own default mode. If the user puts `--mode` in `launch_args` directly, that override wins.
  - `adapters.<name>.tiers` (map): named launch profiles (typically model selectors) the conductor picks per sub-task. Each entry has `description` (text the conductor reads in its brief to decide when to use the tier) and `launch_args` (appended after the adapter's base `launch_args` so a tier-specified flag wins on conflict). Tier names are free-form. Plans referencing a tier the resolved adapter hasn't configured are rejected at validation. See `docs/ai/swarm.md` § Tiers for the full example.
- `notifications` block: notification settings.
  - `notifications.ntfy.url` (string, default `https://ntfy.sh`): ntfy server URL. Use `https://ntfy.sh` for the public server, or your self-hosted instance URL.
  - `notifications.ntfy.topic` (string, required to enable): topic name to publish to. Leave empty to disable push notifications entirely.
  - `notifications.ntfy.token` (string, optional): access token for private/protected topics.
    When configured, Legato sends push notifications when an agent finishes work, needs input, or when all swarm sub-tasks are idle. Toggle per-agent in the TUI (`n` key) or web UI (bell icon).
  - `notifications.os.enabled` (bool, default `false`): enable OS-native desktop notifications on Linux (via `notify-send`) and macOS (via `osascript`). These fire unconditionally — no per-task toggle.
- `workspaces[].path` (optional string): filesystem path associated with the workspace. Used to pre-fill the working directory in the swarm-init overlay when the selected card belongs to that workspace.
- `voice` block: optional voice-to-tmux dictation settings. When `voice.enabled` is `true`, the `v` key in the agents view starts/stops audio recording. Press `v` once to start, speak, press `v` again to stop — Legato sends the audio to your wyoming-faster-whisper instance and sends the transcribed text into the focused agent's tmux pane (like macros). Audio capture uses `arecord` (ALSA, Linux-only) at 16kHz mono.
  - `voice.enabled` (bool, default `false`): gates the voice feature. When `false`, the `v` keybinding is hidden and the voice service is not constructed.
  - `voice.whisper_url` (string): Wyoming protocol URL of the wyoming-faster-whisper server, e.g. `"tcp://192.168.1.220:10300"`. Bare `host:port` is also accepted. Required when `enabled` is `true`.
  - `voice.autosend` (bool, default `false`): when `true`, sends the Enter key after the transcribed text so the agent immediately processes the message. When `false`, the text is typed into the pane but not submitted. The number of Enter keys is adapter-aware: Codex gets 2 (double Enter to submit), all others get 1.
  - `voice.mic_device` (string, default `"default"`): ALSA device name for audio capture (e.g. `"hw:1,0"`, `"plughw:0"`). Empty falls back to `"default"` (system default mic).
- `ssh_agent_socket` (string, optional): explicit override for the `SSH_AUTH_SOCK` environment variable. When set, Legato sets `SSH_AUTH_SOCK` to this path in its own process environment so all spawned subprocesses (tmux sessions, git operations, workflow steps) inherit it. When unset and `SSH_AUTH_SOCK` is already in the environment, the existing value is preserved. When unset and `SSH_AUTH_SOCK` is empty, Legato auto-detects 1Password's agent socket at `~/.1password/agent.sock` if it exists. This solves the common case where SSH keys stored in 1Password aren't accessible to subprocesses launched by Legato (e.g. `git push` from a workflow agent fails with a username/password prompt).
