# Agent states

What each agent tool tells Legato, how Legato turns that into an agent's
state, and where that falls short. Written for LEG-144; keep it current when a
tool's signals or the mapping change.

The states (`store.Activity*`, `agent_sessions.activity`):

| State | Means |
| --- | --- |
| `working` | A turn is running. |
| `waiting` | A turn is held on the operator: a permission prompt or a question. |
| `background` | The turn ended, and something it started still runs and will wake the session: a shell, a Monitor, a subagent, a session cron. `background_jobs` lists them. |
| `""` (idle) | The turn ended, and nothing will wake the session but a message. |
| `error` | The turn ended on an error the agent tool reported (an API error, a rate limit). |
| exited | No running session (`agent_sessions.status` is not `running`). |

`background_jobs` (`agent_sessions.background_jobs`, JSON; `store.BackgroundJob`)
is what still runs: `kind` (`bash`, `monitor`, `subagent`, `cron`), `id`, `label`
(a shell's description or command, a subagent's description and type, a cron's
prompt), `schedule` for a cron, and `started_at` (when Legato first saw it). It
is in `GET /api/agents` and in the Reeve's watch message. It is kept in the
database, so it survives a restart of Legato. A resumed or reloaded agent is a
new session with none: its background work ended with the old process.

## How the signals reach Legato

- **Claude Code:** the `legato-state` plugin (`internal/engine/hooks/claudemod/state`),
  which Legato writes at every start and loads into every Claude Code session it
  launches, hooks `SessionStart`, `UserPromptSubmit`, `PostToolUse`,
  `PermissionRequest`, `Stop`, `StopFailure`, `PreCompact`, `PostCompact` and
  `SessionEnd`, and pipes each payload into `legato agent hook claude-code`. That
  command keeps only what the state needs (`cli.ParseClaudeHook`) and sends it
  through the host broker (op `agent.hook`) or, outside a sandbox without a
  broker, applies it to the database itself. The host applies it with
  `cli.ApplyClaudeHook`. A sandboxed and an unsandboxed agent take the same path
  from the broker on.
- **Claude Code with the native bridge:** the bridge also reports a main turn that
  ended without an answer (`claudebridge.Options.OnTurnEnded`, sent as the
  synthetic hook event `TurnEnded`). No hook fires for an interrupted turn.
- **Codex:** the scripts `legato hooks install --tool codex` writes
  (`UserPromptSubmit`, `PostToolUse` → `working`; `PermissionRequest` → `waiting`;
  `Stop` → idle) into `legato agent state`. The Codex bridge sees
  `turn/started`, `turn/completed` (with status) and `thread/status/changed`; it
  reports a turn that ended `interrupted` or `failed` as `TurnEnded`
  (`codexbridge.Options.OnTurnEnded`), which moves a session still `working` or
  `waiting` to idle or `error`.
- **Chimera with the mod:** the ordered mod stream reaches `ParseChimeraHook`
  and `ApplyChimeraHook`, through broker op `agent.hook` with `kind: chimera`
  inside the sandbox, or directly through the store outside it. The first main
  report sets `hook_source = 'mod'`; stale `agent state` scripts then do
  nothing. Activity, reporting spans, notifications and job cleaning reuse
  Claude Code's state machinery.
- **Chimera without the mod:** with `adapters.chimera.native_bridge: false`, `legato hooks install --tool chimera` writes scripts
  (`UserPromptSubmit` → `working`; `PermissionRequest`, `UserInputRequired` →
  `waiting`; `PermissionResolved` → from its payload; `OnError` → `error`; `Stop`,
  `Interrupt`, `Timeout`, `SessionEnd` → idle) into `legato agent state`, with
  child sessions' reports turned into notifications only.
- **The tmux pane** is read for prompts (`engine/prompt`). The web stream's
  prompt detector (`internal/server/stream.go`) sets `waiting` while it sees a
  dialog and clears it after, through `AgentService.SetAgentActivity`, for an
  agent that reports nothing itself. It leaves a session the plugin reports for
  alone (`hook_source = 'plugin'` or `'mod'`), whose events already report the dialog.

## Evidence

The Claude Code sequences were recorded on 2026-11-12 from Claude Code 2.1.322,
with a hook on every event that appended its payload to a file, in `claude -p`
and in an interactive session in a private tmux server. They are the fixtures
in `internal/cli/testdata/claude-hooks/` and drive `TestClaudeHook*` in
`internal/cli/claude_hook_test.go`. Cases that could not be recorded (a rate
limit, a resume, automatic compaction, a foreground subagent, a subagent's
permission prompt or error) are tested with payloads of the recorded shapes
and say so. Codex and Chimera could not be run where
this was written (Codex is not installed, Chimera's build is hidden by the
sandbox), so their rows come from the code and are marked *from code*.

What the recordings showed:

- `Stop` carries `background_tasks` (`id`, `type` `shell` or `subagent`, `status`,
  `description`, `command` or `agent_type`) and `session_crons` (`id`, `schedule`,
  `recurring`, `prompt`). A Monitor is a `shell` there; only its `PostToolUse`
  (`tool_name: "Monitor"`, `tool_response.taskId`) says it is a Monitor.
- A background job that finishes wakes the session with `UserPromptSubmit`
  whose prompt is a `<task-notification>` naming the job's `<task-id>`. The next
  `Stop` lists what still runs.
- A background subagent's own tool calls fire `PreToolUse`/`PostToolUse` with
  `agent_id`, after the parent's turn has ended.
- `SubagentStop` also fires for Claude Code's own agents (prompt suggestions,
  compaction), with an empty `agent_type`.
- An API error fires `StopFailure` with `error` (`model_not_found` here) and no
  `Stop`.
- A question (`AskUserQuestion`) opens as a `PermissionRequest`, like a tool's
  permission prompt. Both also fire `Notification` (`permission_prompt`).
- Escape during a tool call, and answering "No" to a permission prompt, fire
  nothing at all: no `PostToolUse`, no `Stop`.
- A manual `/compact` fires `PreCompact` and `PostCompact` (`trigger: "manual"`),
  no prompt and no `Stop`. `/clear` fires `SessionEnd` (`reason: "clear"`) then
  `SessionStart` (`source: "clear"`).
- On the machine this was found on, `~/.claude/hooks/legato-stop.sh` dated from
  4 August, before `legato agent stopped` existed: it reported idle at every
  `Stop` without reading the payload, so `background` could not appear. Nothing
  refreshed installed hook scripts. This is the PROP-348 report: the agent did
  start a background wait, and Legato never read the payload that said so.
- The ticket's empty activity and `state_timeline` for agents that were
  working (its third observation) could not be reproduced after the fact. The
  recordings give two causes that fit it: with the old `Stop` script, a turn
  that ends while a background subagent or shell keeps working reports idle,
  and the timeline shows no work while the agent plainly does some; and an
  agent whose hooks were installed only in some projects' `.claude/` reports
  nothing elsewhere (here they were in the home folder, which covers every
  project). The opposite, `working` throughout, is what an interrupt leaves.
  Sandboxed reports reach the host through the broker as unsandboxed ones do
  (a `legato agent state` sent from a sandboxed session during this work was
  accepted), so the sandbox is not a cause. The plugin is loaded into every
  session Legato launches, wherever it runs, and the bridge reports interrupts.

## Claude Code

"Before" is Legato before LEG-144 with the hooks as installed on that machine;
where the repository's current scripts differ, it says so.

| Case | Signals | Before | Now |
| --- | --- | --- | --- |
| A turn running tools | `UserPromptSubmit`, `PreToolUse`/`PostToolUse` | `working` | `working` |
| Waiting on a permission prompt | `PermissionRequest` (+ `Notification`) | `waiting` | `waiting` |
| Waiting on a question | `PermissionRequest` for `AskUserQuestion` | `waiting` | `waiting` |
| Permission allowed | `PostToolUse` | `working` | `working` |
| Permission denied in the terminal | nothing | stays `waiting` | with the bridge: idle (`TurnEnded`); without it, stays `waiting` (gap) |
| Interrupted (Escape) | nothing | stays `working` | with the bridge: idle, or `background` with jobs (`TurnEnded`); without it, stays `working` (gap) |
| Turn ends, `run_in_background` Bash runs | `PostToolUse` (`backgroundTaskId`), `Stop` with `background_tasks` | idle (stale script); `background` with current scripts, no jobs listed | `background`, job `bash` |
| Turn ends, Monitor runs | `PostToolUse` `Monitor` (`taskId`), `Stop` with a `shell` | idle (stale); `background` | `background`, job `monitor` |
| Turn ends, background subagent runs | `PostToolUse` `Agent` (`isAsync`, `agentId`), `Stop` with a `subagent` | idle (stale); `background` | `background`, job `subagent` named by description and type |
| Background subagent's tool calls after the turn | `PreToolUse`/`PostToolUse` with `agent_id` | flips to `working` | unchanged (`background`) |
| Foreground subagent (Task/Agent) | its tool calls with `agent_id`, inside the parent's turn | `working` | `working` |
| A subagent's permission prompt | `PermissionRequest` (with `agent_id`), then its `PostToolUse` | `waiting`, then `working` | `waiting`, then back: `working` in a foreground subagent, `background` for one listed as a job |
| A subagent's API error | `StopFailure` with `agent_id` | n/a (not hooked) | unchanged: the turn running it goes on |
| Forks | not recorded; a fork is a session of its own, which the mod gives no task (see [Claude Code native integration](claude-code-native.md)) | — | not reported |
| A job finishes and wakes the session | `UserPromptSubmit` with `<task-notification>` | `working` | `working`, that job removed |
| Several jobs, one finishes | as above, then `Stop` listing the rest | `working`, then idle (stale) | `working`, then `background` with the rest |
| Turn ends with a session cron | `Stop` with `session_crons` | idle | `background`, job `cron` |
| Context compaction during a turn | `PreCompact`/`PostCompact` (`auto`) | `working` | `working` |
| Manual `/compact` | `PreCompact`, `PostCompact` (`manual`) | unchanged (idle) | `working`, then idle (or `background` with jobs) |
| API error | `StopFailure` (`error`) | stays `working` | `error` |
| Rate limit | `StopFailure` (`error: rate_limit`, from Claude Code's hook reference; not recorded) | stays `working` | `error` |
| Retry within a turn | none (the turn goes on) | `working` | `working` |
| `/clear` | `SessionEnd(clear)`, `SessionStart(clear)` | idle | idle, jobs cleared |
| Resumed after an interrupt or reload | a new session: `SessionStart(resume)`, then the resume note's `UserPromptSubmit` | idle, then `working` | the same; jobs start empty |
| Legato restarted | none | kept (database) | kept, jobs too |
| Sandboxed against unsandboxed | the same hooks; the report goes through the broker in both when it is reachable | the same | the same |
| Old installed scripts beside the plugin | both report | n/a | the plugin's session ignores every `agent state` report (`agent_sessions.hook_source = 'plugin'`) |

## Codex (from code)

| Case | Signals | Today | Correct | What it would need |
| --- | --- | --- | --- | --- |
| A turn running tools | `UserPromptSubmit`, `PostToolUse`; bridge `turn/started` | `working` | `working` | — |
| Permission or `requestUserInput` | `PermissionRequest`; bridge requests | `waiting` | `waiting` | — |
| Turn ends | `Stop`; bridge `turn/completed` | idle | idle | — |
| Background terminal still running | none in `Stop`; Codex does not wake a turn when one ends | idle | idle (nothing will wake it) | If Codex ever wakes a turn for it, a list in `Stop` like Claude Code's |
| Interrupted | bridge `turn/completed` status `interrupted`; whether `Stop` fires is unverified | stays `working` if `Stop` does not fire | idle | Done with the native bridge (`TurnEnded`); without it, a gap |
| API error | bridge `turn/completed` status `failed` with `error` | stays `working`, or idle after `Stop` | `error` | Done with the native bridge while the session is still `working` or `waiting`; a `Stop` that came first leaves it idle |
| Subagents | no hook carries one | — | — | Codex hooks or app-server events for them |
| Compaction, `/clear`, resume | not hooked | unchanged | as Claude Code | — |

## Chimera with the mod (recorded from `859884e7`)

Sixteen real private-tmux streams in `internal/cli/testdata/chimera-mod` replay
through `RunChimeraLink` against both SQLite and the broker (32 cases). They
cover requests, background work, run endings, append-without-turn, new/fork/
switch/resume and exit/Ctrl+C/TERM/HUP. The pinned binary, normalization and
remaining limits are in that folder's README. Synthetic mapping and authority
tests in `chimera_hook_test.go` and `chimera_hook_broker_test.go` complement
them for overlapping requests, malformed input and other hard-to-record cases.

| Signal | Activity and jobs |
| --- | --- |
| `turn.start` | `working` |
| `message.append` | unchanged |
| `permission.request` (`requestId`) | unchanged: interception before automatic arbitration |
| Session snapshot `pendingPermissions` for intercepted requests | link reports `permission.pending`: `waiting` only while human-pending permissions or questions remain |
| `question.request` (`requestId`) | `waiting` |
| `permission.resolved` / `question.resolved` | `working` only when the link-supplied `pendingRequests` is zero; a missing or positive count leaves state unchanged |
| `background.start` (`jobId`, `command`) | add a `bash` job; activity unchanged |
| `background.exit` (`jobId`, `command`) | remove the job; `background` becomes idle if no jobs remain |
| Background `agent.spawn` (`agentId`, `description`, `background: true`) | add a `subagent` job; activity unchanged |
| Foreground `agent.spawn` (`background: false`) | unchanged, no background job |
| `agent.finish` (`agentId`, `description`, `background`) | remove the job if listed; `background` becomes idle if no jobs remain |
| `turn.complete`, reason `stop` | idle, or `background` while listed jobs remain |
| `turn.complete`, reason `error` | `error` |
| `turn.complete`, reason `waiting_for_input` | `waiting` |
| `turn.complete`, any other reason (including interrupted, timeout, cancelled) | idle, even if jobs remain |
| Any event with `isSubagent: true` | ignored; an emitter's own events never change its parent |
| Stale hook script or pane prompt detector after a mod report | unchanged (`hook_source = 'mod'`) |

`agentId` on spawn/finish is the **subject**, not the emitter: it must not be
used to ignore the parent's report. Only `isSubagent` identifies a child
emitter. The link maintains the human-pending request count; the state handler does
not reconstruct it from request IDs. While permission interceptions remain
open, it polls the authenticated snapshot every 250 ms. Failed snapshots are
logged and retried, not interpreted as a prompt. CHIM-69 does not expose the
actual human permission prompt as a separate mod event. Automatic rules return
before interception; System One/reviewer approvals emit resolutions, but a
failed automatic retry probe can leave an interception unresolved. Such a probe
never counts as a human request. `TestChimeraLinkAutomaticApprovalAndHumanPrompt`
covers this faithfully copied sequence through SQLite and the broker; the old
permission recording now supplies a separate human-pending snapshot. Job text uses the same one-line cleaning
and bounds as Claude Code (200-rune labels, 64-rune IDs, at most 32 jobs).

## Chimera without the mod (from code)

| Case | Signals | Today | Correct | What it would need |
| --- | --- | --- | --- | --- |
| A turn running tools | `UserPromptSubmit` | `working` | `working` | — |
| Permission or question | `PermissionRequest`, `UserInputRequired`; `PermissionResolved` | `waiting`, back to `working` | the same | — |
| Turn ends | `Stop` | idle | idle | — |
| Interrupted, timed out | `Interrupt`, `Timeout` | idle | idle | — |
| Error | `OnError` | `error` | `error` | — |
| Subagents | child sessions report with their own `CHIMERA_SESSION_ID` | notifications only; the parent's state is unchanged | `background` while a child runs after the parent's turn | A `Stop` payload that lists running children, or the children's state through the session snapshot |
| Background shells | none known | idle | idle unless Chimera wakes on them | A list in `Stop` if it does |

## Gaps left

- **Claude Code without the native bridge:** an interrupt or a "No" in the
  terminal leaves the agent `working` or `waiting` until its next prompt. No hook
  fires; only reading the pane could tell.
- **Idle that says it is waiting.** An agent that ends its turn saying it waits
  for something, having started nothing that will wake it, is `idle`, which is
  correct. Flagging its last words is not done.
- **Codex and Chimera** as listed above. Neither could be recorded here, so
  their legacy payloads are read from Legato's code, not from a run; the
  Chimera mod table is from the CHIM69 spec until fixtures are recorded.
- **The "No" in the terminal** is reported only as the end of the turn: with
  the bridge the agent goes idle, which is right, since Claude Code then waits
  for a message.
- **Exits the Reeve caused** (LEG-140) are not carried with the state.
- **Jobs written by two hooks at once.** `ApplyClaudeHook` reads the session
  and writes it back. Two `PostToolUse` hooks of parallel background calls that
  run at the same moment can each keep only their own job; the next `Stop`
  lists the whole truth again. A Monitor lost this way shows as `bash` until it
  ends.
- **A late `TurnEnded`.** A bridge reports a turn that ended without an answer
  only after `TurnEndedDelay` (1.5 s) with no new turn begun, since the next
  turn's own prompt reaches Legato on another path. A next turn that starts
  later than that is reported by its `UserPromptSubmit`, which comes after.
- **`/clear` and running jobs.** Whether background shells outlive `/clear`
  was not recorded; Legato clears the jobs then. If one does outlive it, its
  notification wakes the session and reads as `working`.
- **What job text an agent writes** is kept to known kinds (`other` for any
  other), one line of at most 200 characters without control characters, and
  32 jobs (`cleanHook`), since it reaches the Reeve, the API and the terminal.
