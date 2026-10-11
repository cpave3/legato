# Agent tools (the Legato MCP server)

Every agent Legato launches gets a set of Legato's own tools, through an MCP server that Legato adds to the session at launch: `legato mcp serve`. With them an agent can show something in the web UI (an **artifact** in a side panel, or a **card** in the conversation in place of text), read the task it is working on, record a note on it, and message the other agents in its worktree. Nothing is installed by hand and nothing is written to the agent tool's own configuration.

The same server works for Claude Code, Codex and Chimera, and for any later agent tool that speaks MCP. It was built and verified against Claude Code 2.1.296, Codex 0.157.1 and a Chimera with `--mcp-config`.

## How it fits together

```
 agent session, in its tmux pane                                       Legato
 ┌────────────────────────────────────────┐                  ┌──────────────────────────┐
 │ Claude Code / Codex / Chimera          │                  │ SQLite store             │
 │   │ tools/call over stdio              │   writes a row   │  agent_ui_items          │
 │   ▼                                    │─────────────────▶│  agent_ui_item_revisions │
 │ legato mcp serve  (a child of the      │                  └────────────┬─────────────┘
 │ agent tool; finds its session from     │   IPC                         │ read
 │ the pane's environment)                │   agent_ui_changed            ▼
 └────────────────────────────────────────┘─────────────────▶ web server ── WebSocket ──▶ web UI
                                                              GET /api/ui-items/{id} ◀──── (JSON)
```

The server is a short-lived child of the agent tool, like the CLI commands the hooks run. It opens the store, writes, and broadcasts IPC to running Legato instances. It needs no web server: something shown while no browser is open is there when one opens.

Legato is not the MCP host. The agent tool is, and a terminal agent cannot draw UI. So the UI does not travel through the MCP client at all: Legato's own server hands it to Legato, and Legato's web UI draws it.

## The server

`legato mcp serve [--task <id>] [--execution-id <id>]` speaks MCP on stdin and stdout (diagnostics go to stderr). The flags win; otherwise the session is the one named by `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID`, which Legato puts in every pane.

- **Outside a session** (no task ID) it offers no tools, and its instructions say why.
- **Inside one**, the tools are listed at once, and each call looks the session up again: the task must have a running agent session, and a given execution ID must be that session's. The lookup is per call, not at start, because an agent tool starts its MCP servers while Legato may still be recording the session it launched. A refused call returns a tool error and stores nothing.

| Tool | Arguments | What it does |
| --- | --- | --- |
| `show_artifact` | `title`, `content`, `format` (`html` default, or `markdown`), `key` | Stores an artifact and opens it in the side panel. |
| `show_card` | `html`, `fallback_text`, `title`, `key` | Stores an HTML card, drawn in the conversation at the call. |
| `show_components` | `components`, `fallback_text`, `title`, `key` | Stores a card built from Legato's own components (see below). |
| `session_info` | none | Task ID and title, agent tool, swarm role, working directory. |
| `task_context` | none | The task as Markdown: description, linked ticket, repository details, durable notes. |
| `add_task_note` | `body` | Records a durable Markdown note (`task_notes`, source `agent`). |
| `list_peers` | none | Other agents in the same Git worktree. |
| `message_peer` | `to`, `text` | Queues a message to one peer. |
| `broadcast_peers` | `text` | Messages every peer; the result names who was and was not reached. |

A swarm worker's own identity is its sub-task, which has no description or notes, so `task_context` and `add_task_note` act on the swarm's parent task. The peer tools need tmux; without it the other tools still work and those three are not offered.

**Keys and revisions.** Showing again under a `key` the task already used in that placement replaces the item's content as a new revision instead of adding another item. An artifact and a card may share a key; they are different items.

**Results.** A display tool returns text only, with a marker the web UI reads: `Shown as a card in the conversation [legato-ui:ui-3e93f1138d:1]. …`. It returns no structured content on purpose: Claude Code hands its model a result's structured content *in place of* its text, which would hide both the marker and the guidance. The other tools return text and the same result as structured content.

**Schemas.** Tool input schemas are inferred from Go structs. Go slices and pointers infer as "this type or null"; `internal/engine/mcpserver` rewrites those to the type alone, because a tool's schema is passed on to model providers and several of those reject a property that names two types.

## How each agent gets the server

`agents.mcp.enabled` (default `true`) turns injection on for all three adapters, in `configuredAdapters`.

| Agent | How the server is added | Identity reaches the server | Tool names the model sees |
| --- | --- | --- | --- |
| Claude Code | A third plugin, `legato-tools` (`internal/engine/hooks/claudemod/tools/`), installed to `<data_dir>/claude-mod/legato-tools` and added to the pane's `CLAUDE_CODE_PLUGIN_DIRS`. Its `.mcp.json` starts `${LEGATO_BIN} mcp serve`. | Claude Code gives a plugin's MCP server the session's whole environment. | `mcp__plugin_legato-tools_legato__show_card` |
| Codex | Launch overrides: `-c mcp_servers.legato.command=…`, `….args=["mcp","serve"]`, `….env_vars=[…]`. `legato agent codex` passes every `-c` to the session's app server, which is the process that starts MCP servers. | Codex starts an MCP server with a short list of variables; `env_vars` adds the ones in `mcpServerEnv` (session IDs, `LEGATO_CONFIG`, `LEGATO_HOME`, the XDG directories, tmux). | `mcp__legato__show_card` (`legato.show_card` in the native panel) |
| Chimera | `--mcp-config '{"mcpServers":{"legato":{…}}}'`: servers for that session only, merged over the global config (see Chimera's `docs/mcp.md`). | The entry's `env` map (`LEGATO_TASK_ID: env:LEGATO_TASK_ID`) makes Chimera pass its whole environment. | `mcp_legato_show_x00005f_card` |

- The plugin is separate from the bridge mod, so the tools do not depend on `native_bridge`, and a change to it reloads no bridge. Because it is loaded from the pane's environment, a `claude` the person starts by hand in the pane gets the tools too.
- The overrides come before the person's `launch_args`, so an entry of their own for a server named `legato` wins.
- A Chimera from before the flag would refuse to start at all, so the adapter reads `chimera --help` once, at the first launch that needs it, and leaves an older Chimera alone. A `--mcp-config` in the person's own `launch_args` replaces Legato's: Chimera takes the flag once.

**No approval prompt.** Each agent tool is told to run these tools without asking:

- Claude Code: a `PreToolUse` hook in the plugin answers `allow` for `mcp__plugin_legato-tools_legato__.*` and nothing else.
- Codex: the tools are annotated as not destructive and closed-world; Codex runs them without an approval request.
- Chimera: the entry's `trustedTools` lists every tool name (`cli.MCPToolNames()`).

## How an agent learns that the tools exist

An agent should reach for the tools by itself. One text says so, `cli.MCPGuidance`, and it reaches each agent tool's model by the route that tool supports:

| Agent | Route | Why |
| --- | --- | --- |
| Claude Code | The server's own MCP `instructions`. | Claude Code puts a server's instructions in the model's context for every session that loads the server, including one started by hand, after `/clear`, and on resume. |
| Codex | The launch prompt: `-c developer_instructions=…`. | Codex does not pass server instructions to its model, and it keeps MCP tools out of the model's tool list until they are looked up, so tool descriptions cannot do the job either. |
| Chimera | The launch prompt: the first turn (`--prompt`). | Chimera does not read server instructions. |

Adapters whose tool needs the launch prompt implement `service.ToolsPromptAdapter`. `AgentService.SpawnAgent` adds `ToolsPrompt()` to the prompt every launch injects, after the role or general prompt, so a plain Codex session that had no launch prompt now gets one. An adapter returns the text only when its session really gets the server: not when the setting is off, Chimera is too old, or the person's own `--mcp-config` replaced Legato's.

The guidance names tools by their own names, since each agent tool adds a prefix of its own, and it gives concrete triggers: prefer a card to a Markdown table or a list of numbers; prefer an artifact to a reply longer than about a screen; after showing something, say in one sentence what it is.

Checked with a question that names no tool ("compare these three search backends"): Codex, Chimera and Claude Code on its default model each answered with a component card. A small model (Claude Haiku) answered in text wherever the guidance was placed.

**About prompts and messages in general.** A launch-time system addendum has been reliable since it became a launch flag, not typed text: `--append-system-prompt` (Claude Code), `developer_instructions` (Codex, given to the app server by the launcher) and `--prompt` (Chimera). What the native bridges added is reliable delivery of *messages after launch* (the kickoff, peer notices): they are submitted over the session's endpoint instead of being typed. Server instructions go one step further for Claude Code, because they do not depend on Legato typing the launch command at all.

## UI items

`internal/engine/store/agent_ui.go`, migration `063_agent_ui_items.sql`.

- `agent_ui_items`: `id` (`ui-<10 hex>`), `task_id`, `execution_id`, `placement` (`panel` or `inline`), `item_key`, `revision`, timestamps. `(task_id, placement, item_key)` is unique when the key is set.
- `agent_ui_item_revisions`: `(item_id, revision)`, `format` (`html`, `markdown`, `components`), `title`, `content`, `fallback_text`.

`task_id` is the agent's own identity, which is a sub-task ID for a swarm worker, so it has no foreign key: `Store.DeleteTask` removes a task's items and its workers', and `Store.DeleteSubtask` a worker's.

`service.AgentUIService` owns the rules (`internal/service/agent_ui.go`):

- Content is at most 1 MiB of valid UTF-8. An artifact needs a title; a card needs `fallback_text`, the same information as plain text.
- A task keeps 200 items. Past that, the items changed longest ago are removed, so a long session never reaches a point where nothing more can be shown. A card whose item is gone falls back to its text.
- An item keeps its newest 20 revisions.

## Component cards

`show_components` takes a list of components from a fixed catalog (`internal/service/agent_ui_components.go`, `UIComponentGuide`): `heading`, `text` (Markdown), `stat`, `stats`, `key_value`, `table`, `list`, `badges`, `progress`, `bar_chart`, `code`, `divider` and `actions`. Each is a flat object whose `type` says which fields apply; `tone` is `neutral`, `positive`, `warning`, `negative` or `info`. The service checks every component and names the one that is wrong (`components[1] (table): rows[0] has 1 cells; the table has 2 columns`), so the agent can correct the call.

The web UI draws them with its own elements (`AgentUIComponents`). Nothing the agent wrote runs; every value is text. A component card costs the agent far fewer tokens than HTML and always matches the app; HTML is for what the catalog cannot express.

## Web UI

**Cards in the conversation.** `legatoUICall` (`web/src/lib/agentUi.ts`) recognises a display tool call under each agent tool's naming and reads the item marker from its result. `NativeAgentPanel` draws such a call as `AgentUICallCard` in place of a tool row, and it stands alone as a `spawn_agent` call does: a placeholder while the call runs, then the card at the revision that call made, or a line that opens an artifact. A call that failed keeps its ordinary tool row, where the error can be read. The link from call to item is in the transcript, so cards are drawn again after a page reload, a snapshot repair and a resumed session.

**Artifact panel** (`AgentArtifactPanel`). It lists what the session showed as tabs, artifacts first, with a revision stepper, Copy source, Download and Remove.

- On a wide screen it is a resizable column beside the conversation (drag its left edge, or Left/Right on it; the width is remembered).
- Below 1100 px of viewport (1500 px beside the swarm event log) it is a full-screen sheet, as on a phone, above the bottom navigation.
- `useArtifactPanel` decides when it opens: a new artifact from the agent in view opens it, focused on that artifact; one from another agent is counted on that agent's sidebar entry and opens when the person goes there. A card never opens it. Where the panel is a sheet, a new artifact only marks the button, so it cannot land on top of what the person is typing.
- The **Artifacts** button is in the session's own toolbar (the native panel's status bar, the terminal `PromptBar`), with a count and a mark for new ones. A native session counts its artifacts; a terminal session counts its cards too.

**Terminal sessions.** A session shown through its terminal has no conversation feed. Its cards are in the panel beside its artifacts.

**The sandbox** (`SandboxedHtml`, `buildSandboxDocument`). HTML an agent wrote is never trusted: an agent can read hostile text.

- The frame is `sandbox="allow-scripts"` and nothing else. Without `allow-same-origin` its content has an origin of its own: it cannot read Legato's storage (where the auth token is) or call Legato's API as the person, and it cannot open or navigate windows or submit forms.
- A content security policy, placed ahead of everything the agent wrote, allows no network (`default-src 'none'`, `connect-src 'none'`), no frames, no form targets and no `<base>`. Scripts, styles, fonts and images may load from the hosts in `agents.mcp.resource_domains`, and from nowhere else.
- The server never serves item content as a page. It leaves `GET /api/ui-items/{id}` inside JSON, with `nosniff`.
- Every frame carries a **Made by the agent** label that the content cannot remove.

Checked in a browser: script in a frame was refused the parent's storage and document, `fetch` to Legato and to another site, `window.open`, navigating the top window, and a script from an unlisted host.

A frame starts with a small script that gives the content `legato.proposeMessage(text)` and `legato.openLink(url)`, reports its height, and passes link clicks to the host. The messages are JSON-RPC with MCP Apps' method names (`ui/notifications/size-changed`, `ui/open-link`, `ui/message`), and the theme is given as MCP Apps' CSS variables (`--color-background-primary`, `--color-text-primary`, `--font-sans`, …), so the frame is a start on drawing other MCP servers' UI. The host acts only on messages from the frame's own window, and opens only `http` and `https` links, after the person confirms.

**Proposing a message.** A card or an artifact can put text in the person's message box: an `actions` component's button, or `legato.proposeMessage(text)` from HTML. The text is added to what is there, a notice says where it came from, and nothing is sent until the person sends it. It travels as a window event (`proposeComposerText` / `onComposerProposal`), so the native composer and the terminal `PromptBar` both take it without knowing of the panel.

## In the TUI

The TUI cannot draw a card or an artifact, so it counts them and links to the web UI. In the agents view, the selected agent gets a line under the header when it has shown something:

```
Shown in web UI: 2 cards · 1 artifact · latest: Sync design  o open
http://localhost:3080/agents?task=fekz3ove&artifact=ui-885e1b7a26
```

- `o` opens that address in the browser (`clipboard.OpenURL`). It answers for every agent: for one that has shown nothing, or with the web UI off, the status bar says why nothing opened. When no browser can be started (a remote terminal), the status bar gives the address to open elsewhere.
- The address is never cut short, since a partial one can be neither opened nor copied. It shares the first line when there is room, takes a line of its own when there is not, and is left out when the panel is narrower than the address; `o` still works.
- When an agent shows something, the status bar says so (`fekz3ove showed an artifact in the web UI`), whichever agent is selected, and the line updates. Both come from `EventAgentUIChanged`; the counts are also reloaded whenever the agent list is.
- The link is `service.AgentUIWebLink(base, taskID, itemID)`: `/agents?task=<id>&artifact=<item-id>`, naming the item shown last. The web UI selects the agent, opens the artifact panel on that item (a card too, and on a phone, where a new artifact does not open by itself), and drops both parameters from the address.
- The base address is `localWebURL(cfg)`: `http(s)://localhost:<web.port>`, or `web.tls.hostname` in place of `localhost` when TLS is on and a host name is configured. It is empty unless `web.enabled`: the line then reads `web UI is off`. A web UI started separately with `legato serve` while `web.enabled` is false is not linked to.

`internal/tui/agents/shown.go` draws the line; `internal/tui/app_agent_ui.go` loads the summaries (`AgentUIService.Shown`), announces new items and opens the link. The key is `ActionAgentsOpenShown` in the hotkey registry, behind `CapabilityAgentUI`.

## HTTP API and events

- `GET /api/agents/{task_id}/ui-items` — a session's items at their latest revision, without content.
- `GET /api/ui-items/{id}[?revision=N]` — one item with its content and, for HTML, `resource_domains`.
- `DELETE /api/ui-items/{id}` — removes an item and its revisions.

All three need the bearer token. Agents write through the MCP server, never through this API.

`agent_ui_changed` carries `task_id`, `item_id`, `placement`, `revision` and `kind` (`shown`, `deleted`). The MCP server broadcasts it over IPC; both `handleIPCMessage` (TUI) and `handleServeIPCMessage` (`legato serve`, which acts on a shorter list of messages) publish `EventAgentUIChanged`; `Server.StartAgentUIEvents` forwards it to WebSocket clients, where `QuerySyncProvider` invalidates the session's list and the item.

## Configuration

```yaml
agents:
  mcp:
    enabled: true            # default; false gives no session the server or the guidance
    resource_domains:        # hosts an agent's HTML may load scripts, styles, fonts and images from
      - cdnjs.cloudflare.com
      - cdn.jsdelivr.net
      - unpkg.com
      - fonts.googleapis.com
      - fonts.gstatic.com
```

The list above is the default. `resource_domains: []` allows no network at all, so everything must be inline. Entries must be plain host names: a scheme, a path, a port, a wildcard or a policy keyword fails config loading, because the list becomes part of a content security policy. Each allowed host sees the viewer's IP address when content loads from it. The list is read by the web server and by each `legato mcp serve`, which tells the agent which hosts it may use.

A session must be started again to gain or lose the server: agent tools read their MCP servers at start.

## Packages

- `internal/engine/mcpserver/` — a stdio MCP server from a list of tools, over the official Go SDK. It knows the protocol and nothing about Legato. `NewTool[In]` infers the input schema; `Serve` runs until the client goes away.
- `internal/cli/mcp.go` — Legato's tools (`MCPServerOptions`), their names (`MCPToolNames`) and the guidance (`MCPGuidance`).
- `cmd/legato/mcp_cmd.go` — the `legato mcp serve` command.
- `internal/service/agent_ui.go`, `agent_ui_components.go` — `AgentUIService` and the component catalog. `task_note.go` — `AddTaskNote`, shared with the workflow `task.note` step.
- `internal/engine/store/agent_ui.go` — persistence.
- `internal/server/agent_ui.go` — the HTTP API and the WebSocket forwarder.
- `internal/engine/hooks/` — `claudemod/tools/` and `InstallClaudeToolsMod`; `SetMCPTools` and `SetToolsPrompt` on the Codex and Chimera adapters; `mcp_tools.go` for what they share.
- `internal/tui/agents/shown.go`, `internal/tui/app_agent_ui.go` — the TUI's line and link for what an agent showed.
- `web/src/lib/agentUi.ts`, `hooks/useAgentUI.ts`, `hooks/useArtifactPanel.ts`, `components/AgentArtifactPanel.tsx`, `AgentUICallCard.tsx`, `AgentUIItemView.tsx`, `AgentUIComponents.tsx`, `SandboxedHtml.tsx`, `ArtifactsButton.tsx`.

## Debugging

- `legato mcp serve --task <id>` can be driven by any MCP client over stdio; `claude mcp list` and `codex mcp list` (with the same `-c` overrides) show whether an agent tool connects to it.
- `sqlite3 <db> "SELECT i.id, i.task_id, i.placement, r.format, r.title FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id AND r.revision = i.revision"` lists what was shown.
- `claude --debug` logs the plugin's hook decision: `Hook approved tool use for mcp__plugin_legato-tools_legato__…, bypassing permission prompt`.
- A scratch Legato for trying it without touching real state is set up as in [Claude Code native integration](claude-code-native.md) § Trying it without touching real state. Its agents still run the person's globally installed activity hooks, which call the *installed* `legato`; against a scratch database migrated by a newer build those calls fail with a schema error, which is noise from the scratch setup only.

## Known limits

- Only the web UI draws artifacts and cards. The TUI's agents view says what the selected agent has shown and links to it (see In the TUI); in an attached terminal the person sees the tool call and the one-sentence reply. `agents.mcp.enabled: false` turns the tools off.
- A view can propose a message. It cannot send one, call a tool, or read anything from Legato.
- Legato draws the UI of its own server only. It is not a general MCP Apps host for other servers' `ui://` resources.
- Chimera subagents (`spawn_agent`) read the global Chimera config and do not get the server.
- A sandboxed frame can navigate *itself* to another page. That page gets nothing from Legato and keeps the **Made by the agent** label, but it is no longer the agent's content.
- For Codex, the guidance is the session's `developer_instructions`, which replaces a `developer_instructions` of the person's own in `~/.codex/config.toml` for Legato-launched sessions (as a role prompt or learned context already did).
- A small model may ignore the guidance and answer in text.
- An agent's items are removed with its task (for a swarm worker, with its sub-task). Until then they stay in the database, within the per-task limits; nothing removes them when the session ends.
