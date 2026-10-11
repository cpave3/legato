# Codex native integration

Codex sessions that Legato launches are connected to Legato directly, not only through their tmux pane. Legato can start and steer turns, interrupt them and answer approvals without send-keys, and the web UI shows the session in the same native panel it uses for Chimera and Claude Code.

Unlike Claude Code, Codex needs no mod: it serves a protocol of its own. `codex app-server` holds the conversation, and the Codex terminal UI can run as one client of it (`codex --remote`). Legato's bridge is a second client of the same server. The app-server protocol is marked experimental and changes between Codex releases; the integration was built and verified against Codex 0.157.1.

## How it fits together

```
 tmux pane                               legato agent codex-bridge                  Legato
 ┌──────────────────────┐              ┌──────────────────────────┐  bound as the  ┌──────────────────────┐
 │ legato agent codex   │── starts ───▶│ codexbridge              │  agent's native│ AgentService         │
 │   becomes (exec)     │              │  ├ WebSocket client ──┐  │  endpoint      │ (messages, interrupt,│
 │ codex --remote ws:// │              │  └ loopback HTTP      │  │◀─── HTTP ──────│  approvals)          │
 └──────────┬───────────┘              └───────────────────────┼──┘─── SSE ──────▶│ web server           │
            │ WebSocket                                        │                   └──────────────────────┘
            ▼                                                  ▼
          ┌──────────────────────────────────────────────────────┐
          │ codex app-server  (one per agent session, loopback,  │
          │ behind a token)   holds the thread, runs the hooks   │
          └──────────────────────────────────────────────────────┘
```

1. **Launch.** With the bridge enabled, `CodexAdapter.LaunchCommand` types `<legato> agent codex -- <codex arguments>` into the pane instead of `codex <arguments>`. The arguments are the same as before: the role prompt as `-c developer_instructions=…`, `launch_args`, the role model, the tier's args.
2. **Launcher.** `legato agent codex` starts `legato agent codex-bridge <task-id>` as a child in its own process group, with its diagnostics going to `<runtime dir>/codex-bridge/<task-id>.log`, and waits for one line on its stdout: `{"type":"ready","url":"ws://127.0.0.1:<port>","token":"…"}`.
3. **Bridge.** The bridge starts `codex app-server --listen ws://127.0.0.1:0 --ws-auth capability-token --ws-token-file <file>` plus the session's configuration arguments, reads the address the server prints (`listening on: ws://…`), joins it as a client, serves the native agent protocol on a loopback port under a secret path, binds itself to the task's agent session, and only then announces `ready`.
4. **Terminal.** The launcher replaces itself (`exec`) with `codex --remote <url> --remote-auth-token-env LEGATO_CODEX_REMOTE_TOKEN <codex arguments>`, the token passed in the environment. The pane's foreground process is `codex`, exactly as in a session without the bridge. Because `exec` keeps the process ID, the bridge's parent is now Codex itself: it exits when Codex does, and takes the app server with it.
5. **One conversation, two views.** The terminal UI opens a thread on the server. The bridge follows that thread, so the terminal and the panel show the same turns, and either can drive them.

If anything on the way fails (no `codex` able to share an app server, the bridge not ready within 30 seconds, the bridge exiting), the launcher says why on stderr and starts plain `codex <arguments>`: the session is then driven through its terminal, as it always was.

### Why the configuration goes to the app server

A terminal UI attached with `--remote` does not apply its own `-c` overrides to the thread: the server owns the configuration. `-c developer_instructions=…` given only to the terminal has no effect, verified. So the launcher gives the app server every `-c`/`--config`, `--enable` and `--disable` argument of the session (and still passes them all to the terminal, harmlessly). `--model` does work from the terminal, which sends it with the thread it opens.

Hooks from `~/.codex/hooks.json` run inside the app-server process, not the terminal's. The app server is a child of the bridge, which is a child of the launcher in the pane, so it inherits `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID` and the activity hooks keep working. This is also why each agent session has an app server of its own.

### The update notice

Codex opens with a menu when a newer version is out, and nothing gets past it until a person answers. An agent Legato starts may have nobody at its terminal, so the launcher passes `-c check_for_update_on_startup=false` ahead of the session's own arguments. `launch_args: ["-c", "check_for_update_on_startup=true"]` turns it back on (a later override wins).

## Packages

- `internal/engine/codexbridge/` — the bridge. `client.go` is a JSON-RPC client over WebSocket (Codex omits the `jsonrpc` member). `session.go` keeps the conversation in the shape the panel reads; `tools.go` maps Codex items to native tool calls; `requests.go` maps approvals and questions; `commands.go` delivers messages and interrupts; `describe.go` and `model.go` serve models and settings; `stream.go` is the SSE replay ring; `lockfile.go` advertises the bridge. It is self-contained: engine packages may not import each other, so the small protocol-server pieces it shares in spirit with `claudebridge` are its own.
- `internal/cli/codex_bridge.go` — `RunCodexBridge`: starts the app server, hosts the bridge, binds and unbinds the agent session, broadcasts `agents_changed` IPC. `CodexAppServerAddress` reads the server's banner.
- `cmd/legato/agent_codex.go` — the `legato agent codex` launcher and the `legato agent codex-bridge` host.
- `internal/service/codex_bridge.go` — re-discovery from the lockfile, and `RecoverNativeEndpoints` (see Recovery).
- `internal/engine/hooks/codex.go` — `SetNativeBridge` switches the launch command; `BindsNativeEndpoint` makes launch messages wait for the bridge.

## Following the terminal's thread

The bridge does not create the thread; the terminal UI does, and the bridge learns of it from the server.

- **`thread/started`** is sent to every client. A thread that is not `ephemeral` and has no `parentThreadId` is the session's conversation; Codex also runs short-lived helper threads on the same server (one names the session, `threadSource: "thread_title"`) and subagent threads, which are ignored.
- **Joining.** `thread/resume` subscribes a client to a thread's events and returns its history, but fails with `no rollout found` until the thread's first turn has started: a new thread has nothing saved. So the bridge joins when the thread turns `active` (`thread/status/changed`, which unjoined clients do receive), retrying for about three seconds, or at once when the terminal resumed a thread with history (`codex resume`). Until joined it hears only `thread/started`, `thread/status/changed` and `thread/name/updated`.
- **After joining** it receives `turn/started`, `item/started`, `item/completed`, `item/agentMessage/delta`, `thread/tokenUsage/updated`, `turn/completed`. The history returned by the join is announced as events too, since nothing of it was known before.
- **A new thread in the terminal** (`/new`) replaces the conversation: the bridge clears its state and emits `run_finished` with reason `reset`, on which the panel re-reads the snapshot. The binding is under the Legato task ID, not the thread's, so it survives this.

## Behaviour

**Messages.** To an idle thread a message is `turn/start`; during a turn it is `turn/steer` with the turn's ID, which folds it into the turn. A steer the server refuses (the turn ended meanwhile) becomes a `turn/start`. `turn/start` works on a thread the bridge has not joined, which is how a message can open a thread's first turn; the bridge joins right after. The bridge never answers "busy".

**Messages before the terminal is ready.** The agent is bound before the terminal is even started, so the panel is native from the first moment and can show what the terminal asks on its way up. A message sent before the terminal has opened a thread is held, shown in the conversation at once, and delivered as one turn when the thread appears. The bridge reports the directory it was launched in as the session's working directory until the thread says where it works; without one, Legato's message path refuses a message before it reaches the bridge.

**What the person typed.** Codex reports a turn's input back as one `userMessage` item: every piece of text that was sent, run together. The bridge remembers what it sent, so a held message is not shown a second time and context attached to a message (`@path` snapshots, appended with `?append=true` and sent as extra text inputs of the next turn) is not shown as part of what the person wrote.

**Tool calls.** `commandExecution` becomes a native `bash` call with the login-shell wrapper removed (`/usr/bin/zsh -lc 'git status'` → `git status`), its output and exit code. `fileChange` becomes a `patch` call carrying each file's path, kind (`add`, `delete`, `update`) and diff, which the panel draws as a diff per file. `mcpToolCall` and `dynamicToolCall` keep their name (`server.tool`) and arguments; `webSearch` is `web_search`. `agentMessage` and `plan` items are assistant text. Review-mode and compaction items are not shown.

**Reasoning.** A `reasoning` item is what the model thought before a reply or a call. The bridge shows its summary: `item/reasoning/summaryTextDelta` as it streams (`reasoning_text_delta`, with a paragraph break where `item/reasoning/summaryPartAdded` opens a further part), and the completed item's `summary` as the thought's final text (`reasoning_text_done`, and a `reasoning` part in the snapshot, ahead of the reply or the calls it led to). A model that lets its reasoning itself be read sends `item/reasoning/textDelta` and a `content` list; that is shown when there is no summary. History read on joining a thread carries its reasoning items too.

Codex makes a summary only when told to. By default the item arrives with an empty `summary` and no deltas (checked with GPT-6-Astra), and nothing is shown. To have summaries:

```toml
# ~/.codex/config.toml
model_reasoning_summary = "detailed"   # or "concise"
```

or, for the sessions Legato launches only, `adapters.codex.launch_args: ["-c", "model_reasoning_summary=\"detailed\""]`: the launcher gives every `-c` to the app server.

**Approvals.** The app server sends an approval to every client as a request (`item/commandExecution/requestApproval`, `item/fileChange/requestApproval`); whichever client answers first settles it, and the others are told by `serverRequest/resolved`. The bridge raises each as a native permission request, so the panel's card and the terminal's own dialog are up together. A command approval lists the server's `availableDecisions` as options: **Allow once** (`accept`), **Allow for this session** (`acceptForSession`), **Allow, and don't ask again for commands that start with …** (`acceptWithExecpolicyAmendment`), handed back to the server unchanged. A file-change approval shows the change and offers once or for the session. Denying sends `decline` where the request can be declined; a request that offers only `cancel` is cancelled, which ends the turn. Unlike a Claude Code request, an approval is raised whether or not a viewer is attached: it is in the snapshot for a panel opened later.

**Denial feedback.** Codex's decisions carry no message, so feedback is sent as the person's next message: into the turn still running after a decline, or as a new turn once a cancelled turn has ended.

**Questions.** `item/tool/requestUserInput` becomes a native question request (the same card as Claude Code's `AskUserQuestion`), answered with `{answers: {<id>: {answers: […]}}}` or, when dismissed, with no answers. This path follows the protocol's types; no live session produced such a request during verification.

**Other requests.** `mcpServer/elicitation/request`, `item/permissions/requestApproval` and requests about other threads (a subagent's) are not answered by the bridge. The terminal shows each of them, and the panel shows the terminal's prompt (see below).

**Interrupt.** `turn/interrupt` with the turn's ID. The turn completes as `interrupted`.

**Context usage.** `thread/tokenUsage/updated`: the last request's `totalTokens` is the fill, `modelContextWindow` the window.

**Model and effort.** The selector lists `model/list` (hidden models left out), the thread's own model first. Codex takes a model and an effort with a turn, so a choice made in the panel goes with the next turn Legato starts (`turn/start` `model`, `effort`) and holds for the thread from then on; a turn typed at the terminal before that still uses the thread's own. `thread/settings/updated` keeps the panel in step with a change made in the terminal. Effort levels offered are the chosen model's own.

**Not supported from the panel.** Chimera-style modes: the bridge lists none and answers `409` to a change.

## Legato's tools in a session

With `agents.mcp.enabled`, `CodexAdapter.LaunchCommand` defines Legato's MCP server for the session through configuration overrides, ahead of the person's `launch_args`: `-c mcp_servers.legato.command="<legato>"`, `-c mcp_servers.legato.args=["mcp","serve"]` and `-c mcp_servers.legato.env_vars=[…]`. Nothing is written to `~/.codex/config.toml`. Because the launcher gives every `-c` to the app server, the server process that starts MCP servers gets them, bridge or no bridge.

Checked against Codex 0.157.1:

- Codex starts an MCP server with a short list of variables. `env_vars` names the ones to pass on; a name that is not set is skipped. Legato passes the session IDs and what locates the same Legato (`LEGATO_CONFIG`, `LEGATO_HOME`, the XDG directories, tmux).
- The tools run without an approval request (they are annotated as not destructive and closed-world).
- Codex does **not** give a server's MCP `instructions` to its model, and it keeps MCP tools out of the model's tool list until they are looked up. So the guidance about the tools is part of the launch prompt (`developer_instructions`), through `service.ToolsPromptAdapter`; a plain session, which had no launch prompt, now has one.
- The bridge names an MCP call `<server>.<tool>` (`legato.show_card`), which is what the native panel recognises.

See [Agent tools](agent-mcp.md).

## Prompts only the terminal shows

Some prompts never reach any client as a request: the update notice, a directory or hook trust question on a new repository, a picker opened in the terminal. The native panel asks the server every two seconds what the agent's terminal is showing (`detect_prompt`), and the prompt detector reads any numbered menu with a cursor on one of its options (`internal/engine/prompt/menu.go`, prompt type `menu`), whichever tool drew it. The panel shows it as **`<Tool> is asking in its terminal`** with the text above the menu and a button per option; choosing one sends the keys that move the cursor there and confirm (`Down Down Enter`), and **Cancel (Esc)** is offered when the menu's hint mentions it. **Open terminal** switches the agent to its terminal view.

The card is hidden while the agent has a request of its own open, since the terminal shows that same request as a dialog. It applies to every native session, so Claude Code's workspace trust question and pickers are covered too. A prompt that is not a numbered menu (free text, a form) is not detected: use the terminal.

## Recovery

- **Legato restarts.** The app server and the bridge are children of the pane, not of Legato, and the binding is stored in the database, so a session stays native across a restart of the TUI or the web server. When Legato starts, `RecoverNativeEndpoints` runs once in the background over every running agent, for Chimera, Claude Code and Codex alike: a binding whose endpoint still answers is left alone; an agent with no usable binding is bound to the endpoint its session advertises (a bridge lockfile, a Chimera instance lockfile) when that endpoint is alive and serves the agent; a binding to an endpoint that is gone is cleared, so the agent is shown through its terminal. An agent downgraded to its terminal on purpose is bound again by this pass if its endpoint is still running: Legato keeps no record of the choice.
- **Codex exits.** The bridge notices its parent is gone, clears its binding, removes its lockfile and token file, and stops the app server (on Linux the kernel also signals the app server if the bridge is killed).
- **Downgrade and upgrade.** "Downgrade to tmux" clears the binding while the bridge keeps running. "Upgrade to native panel" (`POST /api/agents/{task_id}/chimera/discover`) re-binds from `<runtime dir>/codex-bridge/<task-id>.json` after verifying the endpoint is loopback, alive and serving that task.
- **Snapshots.** The web server passes a pending request's `plan` and `options`, and `pendingQuestions`, through from the endpoint's snapshot, so a panel that opens or re-reads while the agent is waiting draws the same card a live event would.

## Configuration

```yaml
adapters:
  codex:
    native_bridge: false   # default true; false starts plain `codex`, driven by send-keys
    launch_args: ["--search"]
```

## Debugging

- `<runtime dir>/codex-bridge/<task-id>.log` holds the bridge's stderr. `<task-id>.json` is its lockfile (the endpoint is in `url`) and `<task-id>.token` the app server's token.
- `curl <endpoint>/v1/sessions/<task-id>` shows the bridge's snapshot.
- `codex app-server generate-ts --out <dir>` writes the protocol's types for the installed Codex: the reference for every method and notification named here.
- A second client is easy to attach by hand: any WebSocket client that sends `Authorization: Bearer <token>`, then `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"x","title":"x","version":"0"},"capabilities":{"experimentalApi":true}}}` and `{"method":"initialized"}`.

## Known limits

- The app-server protocol is experimental. A Codex without `app-server`, `--remote` and `--remote-auth-token-env` is started without the bridge.
- A model or effort chosen in the panel takes effect with the next turn Legato starts, not at once.
- Reasoning is shown only when Codex makes a summary of it, which it does not by default (see Reasoning).
- `requestUserInput` questions, MCP elicitation and wider-permission requests were not exercised against a live session; the last two are left to the terminal.
- Subagent threads are not shown in the panel.
- Resolved approval cards are appended after the conversation when the panel re-reads its snapshot, as they are for the other native agents.
- The shell hooks from `legato hooks install --tool codex` still own activity (working, waiting, idle). The bridge does not report it.
