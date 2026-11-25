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
prompt), `schedule` for a cron, `started_at` (when Legato first saw it), `parent`
(the subagent that started it; absent for the agent itself) and, for a
subagent, `state`: `working`, `waiting` (on the operator) or `background` (its
turn ended and work it started still runs). The list is a tree of every
background subagent at any depth and what each started; see § Nested subagents.
It is in `GET /api/agents` and in the Reeve's watch message. It is kept in the
database, so it survives a restart of Legato. A resumed or reloaded agent is a
new session with none: its background work ended with the old process.

`background_shells` (`agent_sessions.background_shells`; `store.BackgroundShell`)
is a different list: every background shell a Chimera agent's mod reported,
ended ones included, with its status and exit code, for the web UI's shell
tiles. A Claude Code agent's shells are kept by its bridge, with their output.
See [Web UI](web-ui.md) § Background shell tiles.

## How the signals reach Legato

- **Claude Code:** the `legato-state` plugin (`internal/engine/hooks/claudemod/state`),
  which Legato writes at every start and loads into every Claude Code session it
  launches, hooks `SessionStart`, `UserPromptSubmit`, `PostToolUse`,
  `PermissionRequest`, `Stop`, `StopFailure`, `PreCompact`, `PostCompact`,
  `SubagentStart`, `SubagentStop` and `SessionEnd`, and pipes each payload into `legato agent hook claude-code`. That
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

## Operator items beside activity

What an agent posts with `for_operator` (an operator item, see [Attention](attention.md) § Operator items) is kept apart from `activity`, which only the hooks set: Claude's Stop hook straight after the call would overwrite it. An agent's open items close on the operator's own turn, which each tool reports differently:

| Tool | The operator's turn | Not one |
| --- | --- | --- |
| Claude Code | `UserPromptSubmit` whose prompt does not open with `[legato ` and is not a `<task-notification>` (`ClaudeHook.OperatorTurn`, set by `ParseClaudeHook`; only the flag travels, never the prompt). A slash command typed in the terminal counts. | Peers' (`[legato peer …]`), the Reeve's and Legato's own tagged messages, background-job wake-ups, a subagent's prompt |
| Codex | A user message the bridge sees reach the thread live (`codexbridge.Options.OnUserMessage`, never the history it loads on joining), reported through `BridgeHost.OperatorTurn`: the pseudo-hook `OperatorTurn` over the broker, which touches no activity | Tagged messages |
| Chimera | The mod's `turn.start` carries no text, so only Legato's own message path counts | — |

Legato's own message path: each place a message the operator wrote enters Legato calls `service.NoteOperatorMessage` once it is delivered: the web message box and keys sent to a native agent (`internal/server/ws.go`), voice to a native agent (`VoiceService.sendNative`), a macro started on a card (`MacroService.Start`) or picked in the TUI. `agentService.RecordOperatorMessage` closes the items of the agent's execution when the text is untagged (`service.IsOperatorText`). For Claude Code and Codex it repeats what the hook or bridge already did, which is harmless.

An agent's end closes its items as `agent_ended` (`agentService.EndExecution`), except an interrupted one's (`endGoneSession` marks the context `withOperatorItemsKept`): resuming it moves them to the new execution (`Store.MoveOperatorItems`), dismissing it closes them, and `SweepInterruptedAgents` ends with `Store.CloseOrphanOperatorItems` for executions that ended while Legato was down. A restart (Reload) closes them: the restarted agent is a new execution.

An interrupted agent that a workflow's `agent.run` step waits on can be resumed like any other (LEG-625): its step is `suspended` meanwhile, not failed, and the resume goes on with the same attempt. Only a swarm's agents are still refused (`swarmOwner`). An interrupted agent's state is `interrupted` whatever it was before; a resumed one starts again from idle. See [Workflows internals](workflows.md) § Suspended steps.

**Needs you from open items.** `agent_sessions` rows read through `agentSessionSelect` carry `open_items`, the open operator items of the session's execution, and `open_done_items`, those of kind `done`; the agent APIs report `open_items` beside `activity` (`/api/agents`, and in `?view=compact` when not zero). A running agent with an open item that is not a done note needs the operator unless it is in error, and its `activity` is left as its tool reported. A done note never does: the agent says it finished, and its glance is Finished (see § Stages). The rule is applied by `AgentSession.Glance` (the TUI agents list through `NeedsOperator`, `glance` in `/api/agents`), by `AgentSession.ShownActivity` for the board card's activity word (`SummarizeRelatedAgents`, so `/api/board` and the TUI strip say `waiting`), and by `store.GetAgentActivityCounts` for `/api/navigation-summary` and the tmux status line. The web agent list and ensemble (`ensembleSection` in `web/src/lib/agentGlance.ts`) take needs-you from `glance`, and the rest of their grouping from activity until they show the glance groups (LEG-446, LEG-448).

## Stages

An agent place's **stage** says where its work stands, apart from what the agent is doing now (activity) and from the card's column. A place is a task's slot, `(task_id, slot)`, or a workflow agent's execution. Go names say `WorkStage`, since "stage" was taken (preparing agents, review stages). `internal/service/agent_stage.go` holds `WorkStageService`; the rows are in `agent_stages`, the history in `agent_stage_events` (see [Database](database.md) § Agent stages).

| Stage | Means |
| --- | --- |
| `exploring` | Reading, searching, answering; nothing changed yet. A new conversation starts here. |
| `planning` | A plan waits on the operator. |
| `implementing` | Changing the code. |
| `reviewing` | The work is up for review. A reviewer slot starts and stays here. |
| `finished` | The work is done. |

**Order of trust.** Each signal is an entry point of `WorkStageService`, and each records its `source`:

1. `SetByOperator` (`operator`): any stage. `PUT /api/agents/{task_id}/stage` with `{"stage", "reason"}`.
2. `Report` (`agent`): any stage, with the agent's note.
3. `Milestone` (`milestone`): something that happened to the work. It moves the stage forward only, except `WorkMilestone.Feedback` (review feedback delivered to the author), which may move it back.
4. `Evidence` (`evidence`): an edit or a push by the agent. It moves the stage to Implementing from any other stage, Finished included, and never further. It is ignored for a reviewer slot and for a read-only execution.

`Start` (`start`) resets a place for a new conversation. `SpawnAgent` calls it once the execution is recorded, unless `SessionPolicy` is `resume`, in every process (an agent service no runtime gives the shared stages to, such as the swarm CLI's, resets through stages of its own that publish nothing): Resume, Reload and the sandbox switch keep the stage, and `/clear` starts no spawn. The Reeve has no stage: every signal about it is ignored, and the operator's is refused (`ErrNoWorkStage`). Each change publishes `EventAgentStageChanged` (`AgentStageChangedPayload`), which the web server forwards as the WebSocket message `agent_stage_changed` (`task_id` the agent's address, `stage`, `stage_reason`, `stage_source`, `execution_id` for a workflow agent). No column sets a stage and no stage moves a card.

**Reports.** An agent reports with the MCP tool `report_stage` or `legato agent stage` (over the broker in the sandbox), and a `for_operator` item of kind `done` reports Finished. They run in the agent's `legato mcp serve` or CLI process, or on the host for the broker, so they write through `service.ReportWorkStage` and broadcast the IPC message `agent_stage_changed` (`StageIPCMessage`), which each host turns into `EventAgentStageChanged` (`cmd/legato/ipc.go`). A plain spawn's launch prompt asks the agent to report each change (`stagePrompt`: Finished for an answer that needed no change, Reviewing on opening a pull request or marking a tour ready); a named slot, a swarm agent, the Reeve (its own prompt) and an agent that launches with no prompt are not asked.

**Edit evidence.** An edit counts only when its file is inside the execution's working folder (`InsideWorkingDir`; a relative path is read from that folder), so a plan bundle or a scratch file elsewhere does not. The reason reads `edited <path>` relative to the folder. It comes two ways:

- *Native streams.* `ApprovalWatcher` follows one stream per execution (`watchKey`), so a reviewer slot's stream and the main slot's on one task are apart, and passes listeners a `WatchedAgent` (task, slot, execution, kind, working folder). `WorkStageService.ObserveNativeEvent` is one listener (`cmd/legato/approval_runtime.go`): `EditedPaths` reads a `tool_call_start` whose name is an edit tool, the main loop's mapped `edit`, `write` and `patch` (Claude Code's Edit and Write, Codex's `fileChange`, Chimera's own) or Claude Code's raw `Edit`, `Write`, `MultiEdit` and `NotebookEdit`, which a subagent's calls inside `subagent_event` keep. Paths come from `args.path`, `file_path`, `notebook_path`, `changes[].path` and `movePath`, and the file headers of an apply_patch text in `args.patch`. The Claude Code bridge gives a subagent's edit call `args: {path}` and nothing else. The web server's listener keys the context meter by agent ID (`<task>_<slot>`), so a reviewer's meter is its own, and a reviewer's permission or question is answered in its slot (`agentRequestDetails.Slot`).
- *Claude Code hooks.* `ParseClaudeHook` keeps the path of a `PostToolUse` of Edit, Write, MultiEdit or NotebookEdit (`ClaudeHook.Edited`, never the content), the main loop's or a subagent's, and `ApplyClaudeHook` records it through `service.RecordEditEvidence`, which writes and broadcasts `agent_stage_changed` as reports do. A session with the bridge reports an edit both ways; the second changes nothing.

A replayed stream (a new watch reads from the start of the bridge's ring) does not count its old events again: `EvidenceAt` ignores an edit timed before the place reached its stage, so an edit from before a Finished report leaves it Finished, and a new edit after it makes it Implementing (S9). **Gap:** Codex without its native bridge (`adapters.codex.native_bridge: false`) reports only activity through its hook scripts (`legato hooks install --tool codex`), no tool calls, so it gets no edit evidence and moves on reports and milestones only. Legato never runs git in the agent's checkout to find edits.

**Milestones.** `StartWorkStageMilestones` (`agent_stage_milestones.go`, started with the approval runtime in `cmd/legato/approval_runtime.go`) applies each as an edge, once, when it happens; nothing is re-checked against the current state. Producers that have no event of their own publish `EventWorkMilestone` (`WorkMilestonePayload`: task, slot, a workflow agent's execution, stage, reason, and `Back` for one that may move the stage back, which `WorkMilestone.Feedback` applies):

| Signal | Place | Stage | Reason |
|---|---|---|---|
| `EventPlanChanged` `submitted`, `changes_requested`, `rejected` | the plan's task, main slot | Planning | `plan submitted`, `plan sent back`, `plan rejected` |
| `EventPlanChanged` `approved` | the plan's task, main slot | Implementing | `plan approved` |
| `EventReviewChanged` `ready` (the task from the tour when the CLI's broadcast names none) | the tour's task, main slot | Reviewing | `review tour ready` |
| a review pass starts (`startPass`) | the loop's author task | Reviewing | `review pass N` |
| a review pass starts | the loop's reviewer slot (back, as a resumed reviewer may be Finished) | Reviewing | `pass N` |
| findings delivered to the author (`deliver`, not when posting sets `waiting_for_author`) | the loop's author task (back) | Implementing | `fixing review feedback · pass N` |
| the reviewer submits (`Submit`) | the reviewer slot | Finished | `pass N submitted` |
| `EventPRChangesRequested` (a person's review, not Legato's Apps) | the PR's task (back) | Implementing | `changes requested on PR #N` |
| a tracked branch's PR found open and not a draft (`setBranchPR`, refresh or discovery), opened since the task's work began | the task (back: a new PR after finished work is new work) | Reviewing | `PR #N open` |
| a draft PR marked ready | the task | Reviewing | `PR #N ready for review` |
| one of the task's own PRs merged or closed, leaving every own PR merged or closed with one merged (inherited PRs skipped) | the task | Finished | `every PR merged` |

**An inherited PR is no edge** (LEG-519, LEG-626). A PR is inherited when GitHub says it was opened (`created_at`, kept as `PRMeta.CreatedAt`) before the task's work began (`inheritedPR` in `publishPREdge`; a PR opened in the same second counts as the work's own). The work began at the main slot's newest `start` stage event (`Store.WorkStageStartedAt`, read by `prTrackingService.workBegan`), which Resume, Reload and the sandbox switch do not write, so a PR the agent opened stays its own after one; with no start event, at the running main-slot agent's start. An inherited PR moves no stage when it is newly tracked (found by discovery, by branch, head commit or ticket key, or after a push or `gh pr create`), open or merged, nor when it later merges or closes, and `everyPRMerged` skips it: an agent started in a folder whose branch already has an open PR stays Exploring, keeps its own stage when the operator merges that PR while it works, and becomes Finished only once the PRs it opened have merged. Only what happens to the work moves it: a PR opened since the work began, a draft marked ready (inherited or not), or a push by its execution to a branch whose PR is open (**Pushes** below). A push does not make an inherited PR the agent's own, since pushes are not recorded per execution, so such an agent reports Finished itself (`report_stage`, a done note). With no start time known, or a PR fetched before `created_at` was kept, the PR counts as the work's own. Nothing is re-checked against the agent's activity: a merge of its own last PR while it works still makes it Finished. An ephemeral task links no PRs at all ([PR tracking](pr-tracking.md) § GitHub client and synchronization), so its PRs move no stage: its agent reports Reviewing itself.

The CLI's plan broadcasts (`answer`, `completed`, `withdrawn`) name their task. A review run's author task is the loop's `AuthorTaskID` (`legato.review_loop.author_task`, or the handoff's card), not the run task; a loop a person authors has none and moves only the reviewer slot. A tour finding and a follow-up plan request are not milestones yet.

**Pushes.** Each push through the credential proxy that GitHub accepted reaches `WorkStageService.Pushed` with its session's task and execution and its refs (`service.GitPush`, a push listener of `CredentialChannels`, wired in `cmd/legato/approval_runtime.go`). A push by the task's running session in that execution is evidence (`pushed <branch>`), then, when a pushed branch's tracked PR is open, the milestone Reviewing (`pushed to PR #N`). A push from another execution moves nothing. A push also looks for the pushed branch's PR at once ([PR tracking](pr-tracking.md) § Agents' branches).

**Swarms and workflows.** Their commands run in the agent's own process (`legato swarm …`, `legato workflow complete`), so they write through `service.RecordWorkMilestone`, which broadcasts `agent_stage_changed` as reports do:

| Signal | Place | Stage | Reason |
|---|---|---|---|
| `EventPlanProposed` (the conductor waits in `propose-plan`; on the host) | the parent task | Planning | `swarm plan proposed` |
| `SwarmService.ApplyApprovedPlan` succeeded (not the verdict) | the parent task (the conductor) | Implementing | `swarm plan applied` |
| `Built` | the worker's sub-task (`st-…`) | Reviewing | `built` |
| `Close` of a `reporting` worker (`done`), or `Finish` closing one | the worker's sub-task | Finished | `closed done` |
| `Finish` | the parent task | Finished | `swarm finished` |
| `WorkflowService.Complete` of an agent step | the step's execution (a workflow agent's place) | Finished | `step <id> done` |

A cancelled worker, or one whose agent died, keeps the stage it reached. While a conductor's plan is pending its glance is Needs you whatever its activity (`plan_pending`). The `workflow_changed` IPC of a completed step names that step, not the one the run moved on to. A worker's place is keyed by its sub-task ID, which is no task row; the agents list titles a worker from its sub-task.

**Glance.** `service.Glance` is the one rule that groups agents, from activity, open items by kind, pending plans and the stage:

| Glance | When (a running agent, in this order) |
| --- | --- |
| `needs_you` | waiting, error, an open operator item that is not a done note, or (main slot) a proposed Legato plan or a pending swarm plan of its task (`plan_pending` in `agentSessionSelect`) |
| `working` | working or background |
| `finished` | stage Finished |
| `needs_you` | its wait on the Reeve went unanswered (`ReeveWaitUnanswered`, below) |
| `working` | it waits on the Reeve's answer (`ReeveWaitOpen`) |
| `your_turn` | anything else: idle mid-work, nothing blocking |
| `ended` | the agent is not running |

**Waiting on the Reeve** (LEG-613, `reeve_wait.go`). An agent that asked the Reeve with `message_reeve` and ended its turn is working on the operator's behalf, not their turn. `MessageReeve` takes the asking session's execution and records the wait (`store.OpenReeveWait`, table `reeve_waits`, one row per execution: a second question keeps the first's `since`, and a new or restarted execution starts with none). The glance then derives the rest, with no turn-end detection of its own: the wait shows only while the agent is running and not working or in the background, nothing makes it Needs you, and its stage is not Finished (`message_reeve` also carries reports, so a finished agent that reported reads Finished). `ListAgents` sets `AgentSession.ReeveWaitSince` and `ReeveWait` (`reeveWaitState`): `ReeveWaitUnanswered`, which is Needs you, when no Reeve can answer (no session, `CurrentReeveSession` nil, and `reeve.watch.wake` off: `AgentServiceOptions.ReeveWakes`; with wake on the question starts one), when the Reeve was told and `ReeveAnswerWait` (15 minutes) has passed since, or when it was never told and `ReeveUntoldWait` (an hour, the backstop) has passed since the agent asked; else `ReeveWaitOpen`. Both are constants. The time to answer runs from when the Reeve was told, not from the asking (LEG-623): the watcher holds an agent's message while the Reeve is mid-turn or waiting on the operator, so a busy Reeve has not seen the question yet, and nothing needs the operator. `ReeveWatcher.deliver` records it after a delivery (`markTold`: `reeve_waits.told_at` of the executions whose `WatchAgentMessage` events, which carry `execution_id`, were in the delivered batch; `store.MarkReeveWaitsTold`), and `agentSessionSelect` reads it as `reeve_wait_told_at`. A second question keeps `told_at` as it keeps `since`; a wait that opens again after it ended is not told yet. No Attention item is raised for a wait, open or unanswered: the glance alone puts the agent under Needs you. `AgentSession.WaitingOnReeve()` is that state when the wait is what decides the glance, else "". It ends on the Reeve's answer (`ChangeSetService.watch`: a change-set call that messaged the agent, `agentStartedBy`, clears the wait of the one session its address names, `endReeveWaitOf`: a running execution's ID, else an agent ID, a task for its main slot or `<task>_<slot>`; a task's other slots keep theirs; `reeve_answered`. `SendPeerMessage` delivers to the same session: an execution address keeps its slot, `resolveAgentAddress`, so a reviewer's execution is told and not its task's main agent) and on the operator's turn (`CloseOperatorItemsForOperatorTurn`, which the Claude hook, the Codex and Chimera bridges and `RecordOperatorMessage` all reach; tagged `[legato …]` text is not a turn of the operator's; `operator_turn`). Each change broadcasts IPC `reeve_wait_changed` (`NotifyReeveWaitChanged`), which `cmd/legato/ipc.go` turns into an agents and cards refresh; `ReeveWatcher.Check` broadcasts it too when a wait goes unanswered between two checks (`ReeveWaitsDue` over `store.OpenReeveWaits`: `told_at + ReeveAnswerWait`, else `since + ReeveUntoldWait`), since nothing else changes then; recording `told_at` changes no glance and broadcasts nothing. `/api/agents` gives `waiting_on: "reeve"` and `waiting_since` when `WaitingOnReeve()` is set, and the board card the same pair (`RelatedAgentSummary.WaitingOn`, `WaitingSince`). The web card strip reads `◈ Waiting on the Reeve` with the time waited (`agentStripLook`), the rail row `◈ Waiting on the Reeve · 4m` (`ReeveWaitLine`), the graph `waiting on Reeve` in the Reeve's violet (`agentLook`), and the TUI list `waiting on the Reeve` (`stateReeve`, `Model.stateWord`); unanswered, each says Needs you with how long the agent has waited since it asked: "Waiting on the Reeve for 20 min" (`reeveUnanswered` from `waiting_since`, through `needsYouReason` and the card strip; just "Waiting on the Reeve" under a minute), the graph `needs you`, and the TUI `waiting on the Reeve` under Needs you with the waiting mark and the time since the agent asked (`stateReeveUnanswered`, `Model.stateDuration`). Each names this instance's Reeve as `reeve.name` gives it (`reeveName` and `useReeveName` on the web, `ReeveName.Mid` in the TUI); `waiting_on: "reeve"` stays the protocol's word.

`/api/agents` (full and `?view=compact`) gives each agent `stage`, `stage_reason`, `stage_source`, `stage_since` (absent when no stage is recorded) and `glance`; `?stage=` (comma separated) keeps the agents at those stages. `GET /api/agents/{task_id}/stage-history` lists a place's changes, newest first.

## What the card says needs you

The web board card says why it needs the operator, beside the agent's state. `CardResponse.needs_you` (`service.CardNeedsYou`, absent when nothing is open) is built once per `/api/board` request by `service.NeedsYouByTask` (`card_needs_you.go`) from Attention's open items (`AttentionService.List`: operator items by kind, `items`; `plan_decision` items, `plans` with the first plan's ID) and the pending approvals (`ApprovalService.List`: `approvals`, the count and the first one's ID and short label: an `agent_question`'s question, an `agent_permission`'s title as `permission to run a command`, any other's title). The web UI's `needsYouStrip` (`web/src/lib/boardCard.ts`) picks one strip: an approval (`◆ Needs you · <label>` with **Answer**, which opens it in the rail, `showApproval`), else the operator items (`◆ Needs you · 1 question, 1 action` with **Open**, to `/agents?task=<id>` with the rail unfolded; on a phone it opens the Needs you sheet where the card is, since going to the agent would close it). Each request raises `railState.opens` (`showApproval`, `openNeedsYou`), which `Layout` turns into the sheet opening on a phone, so the same **Answer** works again after the sheet was closed, else a plan (`◆ Plan to approve` with **Review plan**). It is drawn above the review strip (`◆ N changes to review` · **Review**), which is above the agent strip; while the agent itself is waiting or idle the needs-you strip stands in for its strip. The board refetches on `attention_changed`, so an item posted or cleared while the agent idles changes the card.

It also reads the agents (`NeedsYouSources.Agents`, the list `/api/board` already has) and the interrupted agents (`InterruptedAgents.ListInterruptedAgents`): `slots`, each named slot whose agent waits or has a pending approval of its own execution, with that approval's ID and label (`question` for a question); a named slot's approvals are not counted as the main agent's. `agents`, `waiting_agents` and `working_agents` when several main-slot agents run and some, not all, wait. `interrupted` when the task's agent was interrupted and can be resumed. The board's `agent_state` is the main slot's own state (`RelatedAgentSummary.MainState`), so a waiting reviewer no longer turns the strip to Needs you; `SummarizeRelatedAgents`'s `State` still does, for the TUI. The web card adds a strip for each: `◆ Reviewer needs you · question` with **Answer** (or **Open**) above the main agent's own `● Working` and its time (the slot's chip is left out; the tool is in the strip's tooltip); `◆ 1 of 2 agents needs you` · **Open** above `● 1 working`; `‖ Interrupted · Claude Code` · **Resume**, which posts `/api/agents/{task}/resume` as Reload does.

## Foreground safety and exit detection

`tmux.Manager` fails closed for all agent `SendKeys`, `SendKey`, line and
multiline deliveries until `NewAgentService` installs its delivery guard. Each
text/key invocation probes the target's `pane_id` and `pane_current_command`,
checks the running session and tool identity, then sends to that exact pane ID.
It rechecks before Enter, including after the line-delivery gap. Missing probes,
shells and other programs receive nothing; refusal is returned and logged without
message text. A confirmed non-agent program marks terminal delivery interrupted;
a failed probe alone never proves exit. Known tools must match their executable name (`claude`, `chimera`,
`codex`); an unrecognized wrapper/runtime is refused, not guessed to be an agent.
The base64 swarm envelope does not grant permission to type into a shell.

A shell name from `pane_current_command` alone does not confirm exit: sandbox
launchers can leave `sh` in the foreground while `bwrap` and the agent run below
it. `TmuxManager.ShellReturned` checks the pane's original process is a shell
with no child processes, using `ps` on Linux and macOS. A wrapper or failed
probe keeps the session running while terminal input remains refused. Shells
with remaining background children are conservatively kept too.

A confirmed shell return terminalizes the session/execution and fails its exact
unfinished workflow agent attempt. The broker owner reconciles every two seconds,
even without an open UI. Initial startup gets 30 seconds (and clone setup its
existing completion gate); after the tool has been observed, shell return is
immediate. Probe failures are not proof of exit. Shell-only sessions cannot
receive automated messages. Deliberate operator terminal attachment is separate
from automated key delivery.

Bound sessions and adapters expecting a native binding never fall back to keys.
Native errors are reported; launch messages stay pending until accepted and
artifact interactions keep their existing outbox. Raw native prompt-bar keys
are refused (text submissions and interrupts use the native route); TUI native
macros submit text only. Native voice drafts cannot be typed into the terminal.
Only `SendKeysShellCommand` may bypass the agent guard, for the three controlled
spawn/setup bootstrap sites, after verifying a shell receiver.
It pins the initial pane and waits up to three seconds for its shell before
writing launch text: `new-session` can return before the shell has started.
Probe failure or timeout refuses bootstrap; the pre-Enter recheck still applies,
and a partial send is never retried.

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
| A background subagent starts its own | the parent's `PostToolUse` `Agent` with its `agent_id` (`isAsync`, `agentId`) | not listed | listed under its parent; see § Nested subagents |
| A subagent ends its turn with its own still running | `SubagentStop` (with the session's `background_tasks`); no notification to the main loop | listed as running long after | `background`; the parent `background` while the child is `working` |
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

## Nested subagents

Recorded on 2026-11-23 from Claude Code 2.1.330 for LEG-534 (fixtures
`nested-woken-parent` interactive, `nested-background` and `nested-foreground`
from `claude -p`; tests in `internal/cli/claude_hook_nested_test.go`).

- `SubagentStart` and `SubagentStop` fire for a subagent at every depth, with
  `agent_id` and `agent_type`; neither names a parent, a depth or a
  description. A subagent's own tool calls carry its `agent_id`.
- The parent is in the parent's own `PostToolUse` of `Agent`: its `agent_id`,
  with the child's `tool_response.agentId` (`isAsync` for a background child;
  a foreground child's comes only once the child has ended).
- Every `SubagentStop`, like the main loop's `Stop`, carries `background_tasks`
  for the whole session: subagents at every depth and shells subagents
  started, the stopping subagent among them. A subagent whose turn has ended
  while work it started runs is not in it.
- Interactive: a subagent that ends its turn with a child still running does
  not notify the main loop. When the child ends, Claude Code wakes the parent
  with a new `SubagentStart` of the same `agent_id`, and tells the main loop
  only once the parent stops with nothing of its own running. In `claude -p`
  the main loop is told at once, and a nested child's end goes to the main
  loop. Legato depends on neither.
- `subagents/agent-<id>.meta.json`, beside the session's transcript, has
  `parentAgentId`, `spawnDepth` and `description`. It is not yet written when
  `SubagentStart` fires, and its `finished` is set at every stop, so it says
  nothing of whether the subagent runs.

What Legato does (`ApplyClaudeHook`):

| Signal | The tree |
| --- | --- |
| `PostToolUse` that started a job, the main loop's or a subagent's | the job is added under the hook's `agent_id` (`parent`), a subagent `working`; one listed already gains the parent and label it lacked |
| `SubagentStart` of a listed subagent | `working` (one its children woke) |
| `PermissionRequest` with its `agent_id` | `waiting`; its next `PostToolUse` → `working`. A tool call never wakes one whose turn ended: it can be reported after the stop |
| `SubagentStop` with `background_tasks` | the list applied (`listedJobs`): each listed job kept with what Legato knew of it, new ones added; the stopping subagent stays as `background` while anything it started (at any depth) is listed, else leaves |
| A subagent the list leaves out | stays as `background` while Legato has a job under it (so through the stop of its last child, as Claude Code then wakes it), else leaves: a stale or interrupted one goes at the next list |
| `Stop` of the main loop | as `SubagentStop`, and its crons replace the old |
| `<task-notification>` for a job | the job leaves, unless something it started still runs |
| `SessionEnd` | all leave |

A listed subagent whose start Legato never saw has no parent. The hook
command (`ParseClaudeHook`, `subagentParents`) then reads `parentAgentId` from
the subagent's `meta.json`, beside the `transcript_path` the payload names
(IDs of letters, digits, `_` and `-` only, 64 KiB at most, at most 32). Hooks
are processes of their own and can reach Legato out of order, and this is
what keeps the tree when a list comes before the start it lists. Only
background subagents are in the tree: a foreground one runs inside its
caller's work, and its parent is known only once it has ended.

The agent's `activity` stays the main loop's own: `background` until the last
job at any depth ends (the glance counts it as Working). Summaries count the
subagents by what they do. The TUI's: `2 sub-agents working · 1 job`. The web
UI's give the total and each group, a subagent in `background` counted as
waiting, not working: `7 sub-agents · 1 working · 6 waiting on shells · 1 job`
(see [Web UI](web-ui.md)).

Codex reports no subagents. Chimera's mod reports a session's own
`agent.spawn`, but nothing from inside a subagent, so a Chimera subagent's own
subagents are not seen (see § Sub-agent time).

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

## Sub-agent time

A sub-agent's state never changes its parent's (the tables above), but its own
working and waiting time is recorded for the task's reports
(`subagent_activity`; see [Database](database.md) § Sub-agent time). LEG-214.

| Tool | What times a sub-agent | Waiting | Nesting |
| --- | --- | --- | --- |
| Claude Code (state plugin) | `SubagentStart` opens a `working` span (`recordClaudeSubagents`), `SubagentStop` ends it. The main loop's `Stop` ends any not listed in its `background_tasks` (a foreground subagent cannot outlive the turn); `SessionEnd` ends all. Recorded: `several-jobs-and-subagent.jsonl` | `PermissionRequest` with its `agent_id` → `waiting`; its next `PostToolUse` → `working` | `parent_subagent_id` from the parent's `Agent` call or the subagent's `meta.json` (`SetSubagentParent`); a woken subagent gets a new span |
| Chimera with the mod | `agent.spawn` → `agent.finish`, foreground and background (`recordChimeraSubagents`); `session.end` ends all. Recorded: `chimera-mod/subagent.ndjson` | not seen: the mod does nothing inside a subagent | not seen, for the same reason: a subagent's own subagents are missed |
| Chimera without the mod | none (child sessions only notify) | — | — |
| Codex | none: no hook or app-server event carries a subagent | — | — |

Executions of the last two, and every execution from before migration 114, have
`subagent_telemetry = 0`, and reports mark their totals incomplete rather than
whole-tree.

Gaps in what is recorded:

- **Claude Code's own agents** (prompt suggestions, compaction) fire
  `SubagentStop` with no `SubagentStart`; they are not counted, and neither is a
  subagent Claude Code started without a `SubagentStart` (a version older than
  that hook).
- **An interrupted subagent** fires no `SubagentStop`. A foreground one ends at
  the main loop's next `Stop`, a background one at `SessionEnd` or the
  execution's end, so its span can run long.
- **A Chimera subagent's waits** count as working.

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
