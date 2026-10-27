# Claude Code native integration

Claude Code sessions that Legato launches are connected to Legato directly, not only through their tmux pane. Legato can submit prompts, interrupt a turn and answer permission prompts without send-keys, and the web UI shows the session in the same native panel it uses for Chimera.

This works through a Claude Code **mod**: a plugin whose hooks module runs inside the Claude Code process. The mod API is early access and changes between Claude Code releases; the integration was built and verified against Claude Code 2.1.296.

## How it fits together

```
 Claude Code session                       legato agent bridge                      Legato
 ┌──────────────────────┐  spawns        ┌─────────────────────┐   bound as the   ┌──────────────────────┐
 │ Legato mod           │───────────────▶│ loopback HTTP       │   agent's native │ AgentService         │
 │ (hooks/register.ts)  │  commands on   │ + conversation      │   endpoint       │ (messages, interrupt,│
 │                      │◀─── stdout ────│   state             │◀─── HTTP ────────│  permissions)        │
 │                      │                │                     │                  │ web server           │
 │                      │─ POST events ─▶│                     │─── SSE ─────────▶│ (snapshot, stream)   │
 └──────────────────────┘                └─────────────────────┘                  └──────────────────────┘
```

A mod cannot listen for connections: it can only call out and spawn a child. So the direction is inverted compared with Chimera, which serves HTTP itself.

1. **Launch.** `ClaudeCodeAdapter.EnvVars` puts `CLAUDE_CODE_PLUGIN_DIRS=<data_dir>/claude-mod/legato` and `LEGATO_BIN` into the tmux session's environment. Any `claude` started in that pane loads the mod, whether Legato typed the command or the person did. `hooks.InstallClaudeMod` writes the embedded mod to disk when adapters are configured; an unchanged file is not rewritten.
2. **Bridge.** In an interactive session with `LEGATO_TASK_ID` set, the mod's `session.start` hook spawns `legato agent bridge <task-id> --execution-id <id>` for the session's life. The bridge listens on `127.0.0.1:<random port>` under a random secret path, binds itself to the running agent session, writes a lockfile and prints a `ready` line.
3. **Events in.** The mod forwards the engine's events to `POST <endpoint>/mod/events`: a description of the session (`hello`: identity, model, context fill, and the transcript so far), stored transcript rows (`session.append`), streamed reply text and the context fill after each model request (`turn.step`), turn start and end, subagent spawns. Everything from `session.start` onward is queued, so nothing that happens while the bridge is still starting is lost.
4. **Commands out.** The bridge writes newline-delimited JSON commands to its stdout, which the mod reads: `submit`, `queue`, `append`, `interrupt`, `viewers`, `model`, `effort`.
5. **Legato's side.** The bridge serves the native agent protocol Legato already speaks to Chimera (`/v1/sessions/:id`, its SSE event stream, messages, interrupt, permissions). It is bound in the same `agent_sessions.chimera_endpoint` / `chimera_session_id` columns, so `AgentService`, the web server's stream manager and `NativeAgentPanel` drive a bridged Claude Code agent without knowing the difference. The bound session ID is the Legato task ID, not Claude's own, so the binding survives a `/clear`.

`has_chimera_endpoint` on `/api/agents` is therefore true for a bridged Claude Code agent, and the web UI shows the native panel for it. The column and field names still say "chimera"; they mean "native endpoint".

## Packages

- `internal/engine/claudebridge/` — the bridge: conversation state built from mod events, the native-protocol HTTP surface, the SSE replay ring, held permission and question requests (`questions.go`, `permission_options.go`), command output, and the lockfile. Claude Code tool calls are mapped onto the tool names and arguments the panel has cards for (`Bash` → `bash`, `Edit`/`Write`/`Read` → `edit`/`write`/`read` with `path`, `Agent` → `spawn_agent`); other tools keep their name with a one-line summary.
- `internal/engine/hooks/claudemod/legato/` — the mod: manifest, `hooks/register.ts`, and its tests. Embedded in the binary by `internal/engine/hooks/claude_mod.go`.
- `internal/cli/claude_bridge.go` — `RunClaudeBridge`: hosts a bridge, binds and unbinds the agent session, broadcasts `agents_changed` IPC.
- `cmd/legato/agent_bridge.go` — the `legato agent bridge` command. It exits when the Claude Code process that spawned it is gone.
- `internal/service/claude_bridge.go` — re-discovery from the lockfile, used by "Upgrade to native panel".

## The mod, hook by hook

`hooks/register.ts` is the whole mod. Outside a Legato session (`LEGATO_TASK_ID` unset) and in a headless run every hook passes straight through. Its state is module-level and lasts as long as that copy of the module is loaded.

| Hook | What the mod does |
| --- | --- |
| `session.start` | Describes the session (`hello`) and spawns `legato agent bridge` once per process. A second session under the same process (`/clear`, `/resume`) is described to the bridge already running. A background session (`CLAUDE_CODE_SESSION_KIND=bg`) first finds which task it serves from its note, never from its environment (see Sessions sent to the background). |
| `session.end` | On `clear` or `resume`, resets the panel immediately and waits for the replacement ID. Clear reports the identity without erasing new rows; resume describes the replacement history. For every other reason the session is over in this process: the mod leaves a hand-over note and tells its bridge to go (`ended`). |
| `session.append` | Forwards each stored user or assistant row (`row`), with long text, arguments and results cut to bounded sizes. A thinking block goes with what can be read of it, without its signature. Rows the engine writes beside the conversation never leave the session. |
| `turn.start`, `turn.complete` | Report the turn's start and end. At the end it also sends the model and context fill, and submits as a new prompt any message that was folded into the turn but never read. |
| `turn.step` | Applies the model and effort chosen in Legato to the request, forwards the reply text and the thinking before it as they stream (`text_delta`, `thinking_delta`), reports messages the request carried (`user_note`) and the context fill after it (`usage`). Subagent requests pass through untouched. |
| `agent.spawn` | Reports a subagent launched by the main loop (`agent_spawn`) so its activity is grouped under the call that spawned it. |
| `classic.PermissionRequest` | Posts the request to `POST <endpoint>/mod/permissions` and waits inside that call for a decision. This one hook carries tool approvals, plan approval and the model's questions. |

**What the mod posts** (`POST <endpoint>/mod/events`, batched, in order): `hello`, `reset`, `identity`, `row`, `turn_start`, `turn_complete`, `text_delta`, `thinking_delta`, `user_note`, `usage`, `model`, `settings`, `agent_spawn`. Hooks only add to a queue; one pump per bridge sends it, so no hook waits on the bridge. The one exception is `ended`, which an ending session posts itself, because the engine gives it little time.

**What the bridge writes back** (one JSON object per line on its stdout):

| Command | Effect in the session |
| --- | --- |
| `ready` | Names the bridge's URL; the queue starts to drain. |
| `submit` | `$.prompt.submit` as the person's own prompt, or `$.command.run` when the text is `/name args` for a command the session has. |
| `queue` | During a turn: appended as a row the next request reads. With no turn running it is a `submit`. |
| `append` | A row for the next request to read, without starting a turn (an attached file). |
| `interrupt` | `$.turn.abort` on the running turn. |
| `model`, `effort` | Set or clear the override `turn.step` applies. |
| `viewers` | How many native viewers are attached. The bridge itself decides from this whether to raise a request in Legato; the mod only records it. |

**A held permission request.** The mod sends `{requestId, tool, input, suggestions}`, where `suggestions` is the engine's `permission_suggestions`. The bridge answers when someone decides, or straight away when nobody is watching:

| Bridge answers | The mod returns to Claude Code |
| --- | --- |
| `{decision: "ask"}` | Nothing: the terminal dialog decides. Also the answer when the bridge cannot be reached. |
| `{decision: "allow"}` | `{behavior: "allow"}` |
| `… answers: {<question text>: <answer>}` | `allow` with `updatedInput` = the tool's input plus `answers` |
| `… returnInput: true` | `allow` with `updatedInput` = the tool's input unchanged (a plan) |
| `… updatedPermissions: […]` | `allow` with those `updatedPermissions`, in the engine's own form |
| `{decision: "deny", message, feedback}` | `{behavior: "deny", message}`: the bridge's wording, else the bare feedback |

All wording and all mapping from options to engine changes live in the bridge (`permission_options.go`, `questions.go`), where they are tested in Go; the mod only passes them through.

## Behaviour

**Messages.** `AgentService.SendMessage` and `QueueMessage` reach a bridged agent as `$.prompt.submit`, read as the person's own words. A message sent during a turn is appended to that turn and read by its next model request; if the turn ends first, the mod submits it as the next prompt. The bridge never answers "busy". A message of the form `/name args` that names a slash command of the session runs that command.

**Shell commands from the panel.** A `!command` typed in the native composer or sent as a macro runs on the server (`/chimera/bang`), and its result is appended to the session (`append`): the model reads it with its next request, and no turn starts. The mod's `session.append` hook never sees a row the mod appends itself, so the bridge adds the result to its own conversation when it accepts the append (`showAppended`), and the panel draws it as a command card when it reads the snapshot after the command. Only text in the result's shape (`!<command> (exit <code>)` and the output) is shown; an attached file stays hidden. It is in the snapshot only, with no live event: a `user_message` would be drawn as a prompt and mark the session busy.

**Messages at launch.** The brief kickoff and the peer-coordination notice are sent moments after spawn, while Claude Code is still starting; typed into the pane then, the text lands in the prompt box and the Enter is lost. `ClaudeCodeAdapter` implements `service.NativeBindingAdapter`, so `AgentService` holds those messages until the session's bridge is bound and submits them over it. A session that has not bound within 30 seconds (`nativeBindTimeout`: an older Claude Code, a mod that did not load, a dialog nobody answered) gets the message typed, as before. Adapters without the capability keep the fixed `brief_kickoff_delay_ms`.

**Swarm delivery.** `SwarmService` messages, broadcasts, urgent interrupts, conductor event pointers and the adhoc-conductor kickoff use the native endpoint for any participant that has one (Chimera or bridged Claude Code) and send-keys otherwise.

**Resumed sessions.** A session started with `claude --resume` or `-c` describes its transcript when it starts, so the panel shows the earlier conversation. `/resume` inside a running session swaps the conversation without appending rows; the mod sees `session.end` with reason `resume`, waits for the switch and describes the session again, and the bridge tells the panel to reload. The description carries the newest messages that fit a 2 MB budget (tool arguments and results cut to 4,000 characters), because a long transcript sent whole would exceed what the bridge accepts in one request.

**Task launch resumption.** Legato saves the actual Claude session ID and working directory from the mod. The task launch dialog and TUI offer Resume/New for saved Claude sessions. Resume launches `claude --resume <saved-id>` and restores the provider storage directory. It does not inject fresh-session onboarding or replay the brief kickoff. `/resume` and `/clear` update the saved identity without changing the task-ID panel binding. Saved IDs survive bridge shutdown. Child agents and ephemeral tasks do not replace the task conversation. Sessions stopped before identity capture are not imported automatically.

The storage root is the captured `CLAUDE_CONFIG_DIR`, or the launching user's `~/.claude`. If the transcript has been deleted, Claude reports the error in the terminal; Legato does not fall back to a fresh conversation or erase the saved ID. Choose **Start new session** to replace it after the new session reports its identity. A missing or different workspace, or conflicting session/workspace launch flags, returns a launch error before spawning.

**Context usage.** The fill shown in the panel comes from `$.session.usage()`: at session start, after every model request of a turn, and when a turn ends. After `/clear` it is unknown until the new conversation's first reply.

**Permissions.** The mod hooks `classic.PermissionRequest`, which fires when Claude Code opens a permission dialog. While a native viewer is attached, the bridge also raises the request in Legato and holds the mod's HTTP call open until someone decides. Claude Code's own dialog stays open in the terminal the whole time, so either side can answer; a result arriving for the call means the terminal answered, and the request leaves the panel. With no viewer attached the request is not raised in Legato at all.

An allow can carry more than a yes, as the terminal's own dialog does. When Claude Code suggests permission changes for its "don't ask again" choice (`permission_suggestions`), the request lists two options, **Allow once** and one worded from those changes ("Allow, and don't ask again for Bash(npm run:*) in this project"); taking the second hands the suggestions back to the engine unchanged, so they are saved exactly where the terminal's choice would save them. A request with nothing to remember keeps the single Allow. A denial reaches the model worded as Claude Code words a denial at its own dialog ("The user doesn't want to proceed with this tool use … the user said: …"): a bare reason reads to the model as a tool's error, not as the person's word.

**Plan approval.** `ExitPlanMode` raises a permission request that carries the plan as Markdown, which the panel renders, and two ways to approve it: accept edits without asking (`acceptEdits`) or ask before each edit (`default`). Claude Code accepts a plan from a hook only when the allow hands the tool's input back (`updatedInput`); a plain allow is ignored and the dialog stays open. The chosen edit mode travels as a `setMode` permission update for the session. Rejecting with feedback is the terminal's "Tell Claude what to change".

**Questions.** An `AskUserQuestion` dialog is the tool's permission prompt, so it arrives through the same hook. While a native viewer is attached the bridge raises it as a question request, and the panel draws a card with each question's options, descriptions and previews, checkboxes where several answers are allowed, and a field for an answer of the person's own. The terminal dialog stays open as well; whichever side answers first settles it. Answers given in Legato go back to Claude Code inside the tool's own input (`updatedInput.answers`, keyed by the question's text, several choices joined with a comma), which is how the engine reads them from its dialog. Dismissing the card denies the call with a message saying the person chose not to answer. With no viewer attached the question is not raised in Legato at all.

**Thinking.** A response's thinking block reaches the panel as a **Thinking** row (see [web UI](web-ui.md) § Thinking in the native panel). The mod forwards `thinking` chunks of the reply stream (`thinking_delta`), which the bridge emits as `reasoning_text_delta`, and the stored row's `thinking` block, which settles the oldest thought still streaming as `reasoning_text_done` and puts a `reasoning` part in the snapshot. When the stored block has no text, what streamed is kept as the thought; a block with neither is not shown. A subagent's thinking, when it has text, is a line of its card.

How much of this there is depends on the model. Checked with Claude Code 2.1.296: Opus 5.5, Sonnet 5.5 and Haiku 5.5 each returned a thinking block for a short question, streamed no thinking chunk and stored the block with empty text, so there was nothing to show. In a long working session about one block in twenty carried a line or two of text (a summary of where the work stands); those are what the panel shows.

**Interrupt.** `InterruptAgent` calls `$.turn.abort` instead of sending Escape.

**Model and effort.** The panel's model selector and its model settings dialog (Reasoning effort) work for a Claude Code session, and the effort in force is shown beside the model. A choice is an override the mod applies to each request the session's main loop makes (`turn.step`); it holds for that session, ends with it, and is never written to Claude Code's settings. That is deliberate: `/model` and `/effort` save their argument as the person's default for every new session, so the mod does not run them. Two consequences:

- The terminal's own status line keeps showing the session's configured model and effort; the panel shows what requests actually use.
- An override takes a full model ID. The engine no longer resolves aliases (`sonnet`, `opus[1m]`) at that point, and a request naming one fails with "There's an issue with the selected model".

The selector lists the session's own model (the default entry, which clears an override) and the IDs in `adapters.claude-code.models`. `/model <id>` in the composer applies any other ID, and `/model default` resets. Effort offers `low`, `medium`, `high`, `xhigh` and `max`; "Default" is the session's own, known once it has made a request. Subagents keep their own model.

**Not supported from the panel.** Chimera-style modes: the bridge lists none and answers `409` to a change.

**Sessions sent to the background.** `/background` ends the session in its process and continues the conversation, under a new session ID, in a worker process of Claude Code's daemon. Two things go wrong without help, and the mod handles both:

- *The old bridge would keep the task.* The process the session was sent from can stay on as a viewer, so its bridge would stay bound and serve a conversation nobody adds to. On `session.end` the mod posts `{type: "ended"}`; the bridge exits and unbinds. A bridge that starts while another still answers for the task waits up to ten seconds for it to go before it is refused (`bridgeHandoverWait`).
- *The worker does not know its task.* A worker has the environment of the daemon, which is that of whichever Claude session first started the daemon: `LEGATO_TASK_ID` there names some other task, or none. So the ending session leaves a note in the mod's own store (`$.store`, which every process running the mod shares): the Legato variables it held, its working directory, and a mark of its conversation (how many messages, and the last five as role and a hash of their text). A background session takes the note left in the last two minutes, in its directory, whose conversation its own begins with; it asks for up to two seconds, since the two processes start and end at the same moment. It then sets those variables for its own process (`$.env.set`), so the hooks and tools it starts afterwards act for the right task, and starts its bridge with them.

A background session with no note serves no task: it unsets the inherited `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID` and starts no bridge. That is the case for `/fork`, whose original session goes on and keeps the task.

The note a worker took is kept under its own session (`session:<id>`), follows a `/clear`, and is written again each hour the session works; it is good for seven days. It is what a worker the daemon starts again reads, and what stops the process the session was sent from taking the task back: a session that is not in the background starts no bridge while such a note names its task and execution. A worker that ends hands its note on as a hand-over note, as any ending session does.

Checked against Claude Code 2.1.296: a session backgrounded while idle and one backgrounded during a tool call were each served by their worker's bridge within seconds, in a worker whose inherited environment named another task. The store is one JSON file for the plugin, `~/.claude/plugins/store/legato_inline-<hash>.json`, shared by every Legato on the machine.

**Headless runs.** A `claude -p` run inside a Legato session does not start a bridge, so a script calling Claude cannot take over the task's binding. Set `LEGATO_CLAUDE_BRIDGE_HEADLESS=1` to allow it (used for testing).

## Requests the panel can answer

Questions and allow options are part of the native agent protocol, not of the Claude Code bridge: any agent endpoint that emits them gets the same cards. Nothing in the panel checks which agent is on the other end. Chimera's `ask_question` speaks this shape too (its questions are `q1`, `q2`, …, each taking one answer, chosen or typed), so its questions are answered through the same card with no translation in Legato.

| Direction | What | Shape |
| --- | --- | --- |
| event | `question_request` | `requestId`, `questions[]` of `{id, header?, question, options[]{label, description?, preview?}, multiSelect, allowOther}` |
| event | `question_resolved` | `requestId`, and `answers` (`{<question id>: [chosen labels and any typed answer]}`) or `dismissed: true` |
| event | `question_timeout` | `requestId`: no longer answerable here (answered elsewhere, or its turn ended) |
| snapshot | `pendingQuestions[]` | `{requestId, questions[]}`; an absent field means "unknown", an empty list means none |
| snapshot | `status: "waiting_for_input"` | a question is open and no permission request is |
| command | `POST /v1/sessions/:id/questions/:requestId` | `{answers}` or `{dismissed: true}`; `400` when the answers do not fit the questions, `404` when the request is not open |
| event, snapshot | `permission_request.plan` | Markdown the person is asked to approve, drawn in place of the command |
| event, snapshot | `permission_request.options[]` | `{id, label}`: the ways to allow, in place of the single Allow |
| command | `POST …/permissions/:requestId` | `{decision: "allow", option?: "<id>"}`; an allow that names none takes the option that changes least |
| event | `permission_resolved.option` | the option the allow took |

From the browser these travel as the `agent_question` (`request_id`, `answers` or `dismissed`) and `agent_permission` (`decision`, `feedback`, `option`) WebSocket messages, through `AgentService.AnswerQuestion` and `ResolvePermission`. `NativeQuestionCard` (`web/src/components/NativeQuestionCard.tsx`) draws a question from the request alone and can be mounted outside the panel.

## Configuration

```yaml
adapters:
  claude-code:
    native_bridge: false   # default true; false keeps Claude Code sessions tmux-only
    models:                # full model IDs offered in the panel's model selector
      - claude-opus-5-5
      - claude-sonnet-5-5
      - claude-haiku-5-5
```

## Lifecycle and recovery

- A bridge refuses to start when the task is already bound to a live endpoint, after giving that endpoint ten seconds to go: a session sent to the background starts its bridge while the old one is leaving.
- On exit the bridge clears its own binding and removes its lockfile. If it is killed, the stale binding is cleared the next time the panel fails its initial snapshot, exactly as for Chimera, and the next bridge replaces a dead binding.
- "Downgrade to tmux" clears the binding while the bridge keeps running. "Upgrade to native panel" (`POST /api/agents/{task_id}/chimera/discover`) re-binds from the lockfile at `<runtime dir>/claude-bridge/<task-id>.json` after verifying the endpoint is loopback, alive and serving that task.
- When Legato starts, `AgentService.RecoverNativeEndpoints` runs once in the background over every running agent (Chimera, Claude Code and Codex alike). A binding whose endpoint answers is kept; an agent with no usable binding is bound to the live endpoint its session advertises (this bridge's lockfile); a binding to an endpoint that is gone is cleared. So a session stays native across a restart of the TUI or server without anyone pressing Upgrade. A session downgraded on purpose is bound again by this pass when its bridge is still running: the choice is not recorded.
- A panel that opens, or re-reads its snapshot, while a request is waiting draws the same card a live event would: the web server passes a pending permission's `plan` and `options`, and `pendingQuestions`, through from the bridge's snapshot.

## Debugging

- `LEGATO_CLAUDE_BRIDGE_TRACE=<file>` in the session's environment records every event batch the mod posts and every command the bridge writes, one JSON object per line.
- `claude --debug` logs each hook failure and the mod's own `legato:` lines.
- `claude plugin validate internal/engine/hooks/claudemod/legato` reads the mod the way the engine will. `claude plugin test internal/engine/hooks/claudemod/legato` runs its tests in the engine's own test host.
- `curl <endpoint>/v1/sessions/<task-id>` shows the bridge's snapshot; the endpoint is in the lockfile.

## Working on the mod

- The engine's type declarations for the running Claude Code build are written when the `plugin-authoring` skill loads (and beside an installed mod, in `<data_dir>/claude-mod/legato/.claude-plugin/types/`). To type-check the repository copy, point a `tsconfig.json` kept outside the mod at that declarations file plus `hooks/**/*.ts` and `tests/**/*.ts`, with `module: esnext`, `moduleResolution: bundler`, `strict`, `noUncheckedIndexedAccess`, `noEmit`, `types: []` and `jsx: react` with `jsxFactory: h`, then run `tsc -p` on it. `claude plugin validate` does not type-check.
- `claude plugin validate` refuses a module that passes `$` to anything but a function declared at the top of the file, so helpers that take `$` and the state they share live at module scope, not inside `register`.
- A hook's own time is capped at ten seconds per dispatch, but time inside a `$` call is not counted. Anything that waits on a person waits inside `$.http.fetch` against the bridge, never on a plain promise.
- Hooks only queue events; one detached pump per bridge posts them. A hook never awaits the bridge to forward what it saw.
- An interactive session watches a folder loaded through `CLAUDE_CODE_PLUGIN_DIRS` and reloads the mod about a second after a file in it is saved, even while idle. A reload is a fresh load: `register` runs again, `session.start` fires again, the old copy's bridge child is killed and a new bridge is spawned from `LEGATO_BIN`. Sessions load the installed copy under `<data_dir>/claude-mod/legato`, not the repository's: `InstallClaudeMod` rewrites a file there only when the embedded content differs, so restarting Legato on a binary with a changed mod reloads every running session, and one with an unchanged mod disturbs none. Two consequences: editing `internal/engine/hooks/claudemod/` does nothing until Legato is rebuilt and restarted, and a change to the Go bridge alone leaves running sessions on their old bridge process until the mod next changes or the session restarts.

### What Claude Code accepts from the mod

Checked against Claude Code 2.1.296 with a probe mod in a live session; none of it is stated by the type declarations.

- An `AskUserQuestion` call is answered by `allow` with `updatedInput.answers` keyed by each question's text. The terminal dialog closes and shows the answers as the person's.
- `ExitPlanMode` ignores a plain `allow` from the hook: the dialog stays open. `allow` with `updatedInput` (the input handed back unchanged) is accepted as approval, and a `setMode` entry in `updatedPermissions` sets the mode the session continues in.
- `updatedPermissions` given the request's own `permission_suggestions` behaves as the terminal's "don't ask again" choice: a second matching call is not prompted. The suggestions carry their own destinations, so they may be written to settings files.
- A `deny` message is shown to the model as the tool's error. A model given only the feedback text treated it as coming from the tool and disregarded it, which is why the bridge words denials as the person's.
- `turn.step` yields `thinking` chunks beside `text` ones, each with the index of its content block; a response's thinking block is index 0 and its reply the next. The stored thinking block has `thinking` and `signature`; the three current models left `thinking` empty.
- The `Stop` hook's payload lists in-flight work under `background_tasks` (`type: "shell"`, `status`, `command`). When that work finishes the session is woken by a prompt that fires `UserPromptSubmit`. The shell hooks use this for the `background` activity; see [CLI](cli.md).

## Trying it without touching real state

Every piece honours Legato's usual overrides, so a throwaway setup needs only a config file and two environment variables:

```bash
export LEGATO_CONFIG=/tmp/lg/config.yaml      # db.path and data_dir under /tmp/lg
export XDG_RUNTIME_DIR=/tmp/lg/run            # IPC sockets and bridge lockfiles
export TMUX_TMPDIR=/tmp/lgt                   # a tmux server of its own; keep the path short
env -u TMUX legato serve --port 3987
```

Those overrides isolate Legato only. A Claude Code session started this way still uses the person's real `~/.claude`: commands that save a preference (`/model`, `/effort`, accepting a workspace trust prompt) change their actual defaults. Do not run them in a scratch session. `/background` and `/fork` hand a scratch session to the person's real Claude Code daemon: stop and remove it afterwards (`claude stop <id>`, `claude rm <id>`).

A fresh database has no board columns, so nothing can be created in it yet: put `board.columns` in the throwaway config and run `legato columns sync` once, then `legato task create "<title>"`.

Spawn an agent with `POST /api/tasks/{id}/agent` (`{"agent_kind":"claude-code","working_dir":"..."}`), then open `/agents`. The agent should report `has_chimera_endpoint: true` within a few seconds of Claude Code starting. The web UI's token is in `<data_dir>/auth-token`.

Three things that go wrong otherwise:

- **Socket paths have a length limit** (about 100 characters), and both `XDG_RUNTIME_DIR` and `TMUX_TMPDIR` hold sockets. Keep both short; a directory under `/run/user/<uid>/` works.
- **Started from inside a Legato session**, the throwaway Legato inherits that session's environment. Clear `CLAUDE_CODE_PLUGIN_DIRS`, `LEGATO_TASK_ID`, `LEGATO_EXECUTION_ID`, `LEGATO_SOCKET` and `LEGATO_BIN` first (`env -u …`): the adapter appends its mod to an existing `CLAUDE_CODE_PLUGIN_DIRS`, so the scratch session would otherwise load the installed mod as well and start two bridges.
- **Build to a path of its own** (`go build -o /somewhere/legato ./cmd/legato`). Rebuilding `./legato` restarts a Legato running under hot reload and reloads the mod in every live session.

To drive a session without the web UI, a mod of a few lines loaded with `CLAUDE_CODE_PLUGIN_DIRS=<folder> claude` in a tmux session of its own answers most "what does the engine do with this" questions in a minute; the facts under "What Claude Code accepts from the mod" came from one.

## Legato's tools in a session

A third plugin, `legato-tools` (`internal/engine/hooks/claudemod/tools/`), gives a session Legato's own MCP server, `legato mcp serve`: tools to show an artifact or a card in the web UI, read the task, record a note and message peers. It is an ordinary plugin, not a mod, and does not need the bridge mod: a `.mcp.json` that starts `${LEGATO_BIN} mcp serve`, and a `PreToolUse` hook that allows that server's tools (`mcp__plugin_legato-tools_legato__.*`) without a prompt. `configuredAdapters` installs it to `<data_dir>/claude-mod/legato-tools` when `agents.mcp.enabled` and adds the folder to the session's `CLAUDE_CODE_PLUGIN_DIRS`.

**Reading a pasted image without a request.** The plugin has a second `PreToolUse` hook, for `Read`. Claude Code asks the person before it reads a file outside its working directory, and an image the person pasted for the session (see [Web UI](web-ui.md) § Pasting an image for an agent) is such a file: the request would ask them to approve what they just did. The hook puts the question to `legato agent allow-read`, which prints an allow decision only when the file is really in this task's folder of pasted images (`service.IsPastedImage`): the path is resolved first, so a path that climbs out of the folder, a link in it that leads elsewhere, and another task's folder all get no answer, which leaves the decision to Claude Code's own rules. The hook starts Legato only when the payload names `pasted-images`, so every other read costs one shell and nothing more; it does nothing outside a Legato session (`LEGATO_BIN` unset) and always exits 0.

Checked with a live session: a pasted image was read with no request, and a file at `…/outside/pasted-images/shot.png` still raised one.

Checked against Claude Code 2.1.296:

- A plugin's MCP server is started with the session's whole environment, so the server finds its session from `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID` with nothing passed to it. `${VAR}` in `.mcp.json` is expanded from that environment.
- The server's MCP `instructions` reach the model, in every session that loads the plugin. That is how the agent learns the tools exist and when to use them unasked; nothing is added to the launch command for it.
- The model is given a tool result's structured content *in place of* its text when both are present, which is why the display tools return text only.
- MCP tools are kept out of the model's tool list until it looks them up (`ToolSearch`), so a tool's description is not seen until then.

See [Agent tools](agent-mcp.md).

## Reporting usage to Manticore

A second mod, `legato-manticore` (`internal/engine/hooks/claudemod/manticore/`), rides beside the bridge mod. Claude Code calls its provider directly, so Manticore cannot see its requests as a proxy; this mod tells a Manticore instance what each request cost after the fact. It is separate from the bridge mod and does not need it: with `native_bridge: false` a session still reports.

```yaml
manticore:
  url: http://192.168.1.220:3456
  api_key: ${LEGATO_MANTICORE_KEY}   # a Manticore Client's key
```

With that block set, `configuredAdapters` installs the mod to `<data_dir>/claude-mod/legato-manticore`, adds the folder to the session's `CLAUDE_CODE_PLUGIN_DIRS`, and sets `LEGATO_MANTICORE_URL` and `LEGATO_MANTICORE_API_KEY` in the session's environment. Without it the mod is neither installed nor loaded.

**What is reported.** One record per model request that got a response, subagents' requests included, posted to `POST <url>/v1/usage`:

| Field | From |
| --- | --- |
| `model` | The model that answered, as the API names it (`turn.step`'s `usage.model`). |
| `prompt_tokens` | `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`: the whole prompt, as Manticore counts one. |
| `cache_read_tokens`, `cache_creation_tokens` | The API's cache counts, kept apart so cached volume can be told from fresh. |
| `completion_tokens` | `output_tokens`. |
| `latency_ms`, `time_to_first_token_ms`, `started_at` | Measured by the mod around the request. |
| `finish_reason` | The stop reason (`end_turn`, `tool_use`, ...). |
| `upstream` | Always `anthropic`: the label the request appears under in Manticore's upstream breakdown. |
| `id` | `<claude session id>:<turn id>:<step index>`, so a report sent twice is stored once. |

The `X-Session-Id` header is the Legato task ID, so a task's requests stay grouped across `/clear` and `/resume`; outside a Legato task it is Claude's own session ID. No prompt, reply, tool input or file content is read or sent, and the request itself is never touched: every chunk and response passes through as it came.

**Delivery.** The hook only queues; reports go out one at a time from a detached sender, so a slow or unreachable Manticore never delays a turn. A report Manticore could not take (unreachable, `5xx`, `408`, `429`) stays queued and goes out again with the next request's, at most 200 records per report; one it turned down (`4xx`: invalid, an unknown key) is dropped. What is still queued when the session ends gets one last attempt. The queue lives in memory and holds 2,000 requests; past that the oldest are let go.

**Not reported.** Requests that got no response (an API error, an interrupt before the first message). The request a compaction makes, because the engine reports its token counts without naming the model that ran it. Any request the engine makes outside a turn. Cost in dollars: Manticore records tokens only.

**Reading it back.** The TUI's agents view shows the tokens the instance recorded today, in and out, and the ensemble draws the last 24 hours. That is read from the instance's dashboard API by `internal/engine/manticore`, not by the mod, and it counts every Client of the instance. See [TUI](tui.md) § Token usage.

**Testing.** `claude plugin test internal/engine/hooks/claudemod/manticore` runs the mod's tests; `claude plugin validate` on the same folder lists what it hooks and calls. To watch a real session, set the three variables and `CLAUDE_CODE_PLUGIN_DIRS` by hand and run `claude -p ... --debug-file <file>`: each report is a `$.http.fetch (legato-manticore)` line.

## Known limits

- A background worker's MCP servers are started with the daemon's environment before the mod can correct it, so Legato's tools (`legato mcp serve`) in a backgrounded session may act for another task, or offer nothing. The shell hooks and the bridge do get the task's own variables.
- The usage mod's variables (`LEGATO_MANTICORE_URL`, `LEGATO_MANTICORE_API_KEY`) are not in the hand-over note, which is a file on disk; a worker reports with the ones it inherited.
- The process a session was sent to the background from was only seen to exit. That it also ends its session when it stays on as a viewer (the first `/background` on a machine with no daemon running) is assumed, not checked.

- Thinking is shown as far as the model lets it be read, which for current Claude models is seldom (see Thinking above). A resumed session's earlier thinking is not shown: the transcript read back at start carries text and tool calls only.
- The mod API is early access. A hook that fails is skipped by the engine, so a breaking change degrades the session to tmux-only behaviour rather than stopping it.
- The shell hooks from `legato hooks install --tool claude-code` still own activity (working, waiting, background, idle). The mod does not report it, so a session without those hooks shows no activity however well the native panel works.
- MCP elicitation dialogs, the workspace trust prompt and Claude Code's own pickers (`/resume`, `/model`) do not reach the mod as requests. The panel shows the ones drawn as a numbered menu with a cursor through its terminal-prompt card (see [web UI](web-ui.md) § Terminal prompts in the native panel), which presses the keys for the chosen option; anything else (a form, free text) is answered at the terminal.
- A question's free-form "Chat about this" has no card equivalent: dismiss the card and reply in the composer.
- A resumed session hydrates the panel from a transcript read back, which carries no provenance, so engine-injected user rows are filtered by their opening tag.
- A launch message that falls back to typing after the 30-second wait can still land in a dialog Claude Code is showing (the workspace trust prompt for a folder it has not seen), exactly as every launch message did before.
