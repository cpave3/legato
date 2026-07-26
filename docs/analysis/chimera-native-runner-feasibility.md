# Chimera Native Runner Feasibility

## Summary

It is worth adding a first-class Chimera integration that uses Chimera's
structured HTTP/SSE interface instead of controlling Chimera through tmux.

Chimera is likely the best first agent harness to move off terminal-driven
execution. Its SDK and server expose most of the primitives Legato will
eventually need for durable local and cloud-agent execution:

- typed lifecycle and tool events;
- resumable event subscriptions;
- explicit permission requests and decisions;
- interrupts and cancellation;
- session creation, persistence, resume, fork, and rewind;
- mid-run message injection;
- model and mode selection;
- usage and context-window reporting;
- task-list progress;
- subagent events; and
- explicit completion reasons.

This work should be framed as a **Chimera Native Runner**, not merely an
enhancement to Legato's existing Chimera CLI adapter. It would provide an
immediate improvement to Chimera support while also serving as the reference
implementation for Legato's future general runner architecture.

## Recommendation

Implement a native Chimera runner before pursuing Docker or distributed
workers.

The runner should communicate with Chimera's HTTP/SSE server directly from Go.
It should not embed Node solely to consume the TypeScript SDK.

The existing tmux-based Chimera integration should remain available as a
compatibility and diagnostic fallback.

For example:

```yaml
adapters:
  chimera:
    transport: native # native | tmux
```

The term `native` is preferable to `sdk` in user-facing configuration because
Legato will most likely consume Chimera's protocol through a small Go client
rather than importing the TypeScript package itself.

## Current Integration

Legato currently treats Chimera like other terminal-based coding agents:

```text
AgentService
    └── tmux
          └── chimera CLI
                ├── terminal output
                ├── send-keys input
                └── activity hooks
```

The existing integration provides:

- Chimera CLI launch-command construction;
- task and swarm prompts through `--prompt`;
- Chimera mode selection;
- session naming and resume flags;
- model-tier launch arguments;
- hook-based working and waiting activity updates;
- terminal capture and streaming;
- input through tmux `send-keys`; and
- interruption by sending Escape.

This works, but it inherits the limitations of terminal orchestration:

- agent state must be inferred;
- prompt detection depends on rendered terminal output;
- message delivery has weak acknowledgement semantics;
- terminal UI changes can break integration behavior;
- structured tool and usage information is discarded;
- reconnect and replay are difficult;
- permission requests become generic waiting states; and
- run completion must be inferred from process or session behavior.

## Chimera's Programmatic Surface

Chimera's `@chimera/client` package is a typed client over the same HTTP/SSE
interface used by its own TUI. It does not bypass Chimera's normal agent
behavior, permissions, tools, persistence, or event stream.

The client currently exposes operations including:

- inspect an instance;
- create, list, inspect, delete, and resume sessions;
- fork a session;
- list checkpoints;
- rewind a session and workspace;
- inspect context usage;
- interrupt a run;
- compact context;
- resolve and remember permissions;
- list and modify permission rules;
- list commands, skills, modes, and subagents;
- change mode and model;
- inspect and add filesystem paths;
- append and send messages; and
- subscribe to events using a previous event ID.

Chimera events include:

- `session_started`;
- `user_message`;
- `assistant_text_delta` and `assistant_text_done`;
- reasoning deltas and completed reasoning;
- `tool_call_start`, `tool_call_result`, and `tool_call_error`;
- `permission_request`, `permission_resolved`, and `permission_timeout`;
- `skill_activated`;
- `command_invoked`;
- `mode_changed` and `model_changed`;
- subagent lifecycle and nested subagent events;
- background-process completion;
- `task_list_updated`;
- `step_finished`;
- `usage_updated`;
- `run_finished`;
- compaction lifecycle events; and
- session fork metadata.

Every event envelope contains:

- an event ID;
- a session ID; and
- a timestamp.

This is substantially closer to the interface Legato needs for reliable
orchestration than a terminal byte stream.

## Proposed Architecture

The proposed native path is:

```text
AgentService / RunService
          │
          ▼
   ChimeraRunner
          ├── starts and supervises `chimera serve`
          ├── uses a Go Chimera HTTP client
          ├── consumes structured SSE events
          ├── translates events into Legato run events
          └── persists session and event cursors
```

The compatibility path remains:

```text
AgentService / RunService
          │
          ▼
      TmuxRunner
          │
          ▼
     chimera CLI
```

The native runner should eventually implement the same general contract used
by local, Docker, and remote-worker execution.

## Go Client Rather Than TypeScript Embedding

Legato is implemented in Go, while Chimera's published client is TypeScript.
Adding a Node subprocess or JavaScript bridge purely to invoke the SDK would
introduce:

- an additional runtime dependency;
- another supervised process;
- serialization between Legato and the bridge;
- more packaging complexity;
- more failure modes; and
- an unnecessary indirection over an HTTP protocol.

Instead, Legato should implement a small Go client that follows the TypeScript
client as its behavioral reference.

For example:

```go
type ChimeraClient interface {
    GetInstance(ctx context.Context) (Instance, error)
    CreateSession(ctx context.Context, opts CreateSessionOptions) (SessionID, error)
    GetSession(ctx context.Context, id SessionID) (Session, error)
    Send(ctx context.Context, id SessionID, message Message) (EventStream, error)
    Subscribe(
        ctx context.Context,
        id SessionID,
        since EventID,
    ) (EventStream, error)
    ResolvePermission(
        ctx context.Context,
        id SessionID,
        requestID string,
        decision Decision,
    ) error
    Interrupt(ctx context.Context, id SessionID) error
    Resume(ctx context.Context, id SessionID) error
    DeleteSession(ctx context.Context, id SessionID) error
}
```

The initial client need only cover the subset required by the runner. Advanced
session features can be added after the basic lifecycle is proven.

Longer term, an OpenAPI description or shared protocol conformance suite in
Chimera would reduce the risk of the Go client drifting from the TypeScript
implementation.

## Managed Chimera Lifecycle

For an initial implementation, Legato should start and supervise one Chimera
server process per active agent run.

### Start flow

1. The user or swarm selects Chimera as the agent harness.
2. Legato resolves the task, workspace, prompt, mode, model, and policy.
3. Legato starts:

   ```text
   chimera serve --cwd <workspace> --machine-handshake
   ```

4. Legato parses the handshake containing the URL, session ID, and process ID.
5. It persists the Chimera endpoint and session identity.
6. It connects using the Go HTTP/SSE client.
7. It submits the role prompt and task brief.
8. It consumes and translates structured events until the run terminates.

### Stop flow

1. Legato calls Chimera's interrupt endpoint for an active turn.
2. It waits for a terminal event within a bounded grace period.
3. It deletes or retains the Chimera session according to policy.
4. It shuts down the managed Chimera server process.
5. It records the final execution state.

### Advantages of one server per run

- simple process ownership;
- straightforward failure isolation;
- clear task-to-session mapping;
- easy cleanup;
- working-directory isolation; and
- limited consequences from a server failure.

### Disadvantages

- one server process and listening port per agent;
- greater startup overhead;
- more processes to supervise; and
- less efficient operation for many short runs.

These disadvantages are acceptable for an initial implementation. Once the
native integration is stable, Legato could evaluate one Chimera server per
worker or execution environment with multiple sessions.

Beginning with a shared pool would introduce session ownership, cleanup,
recovery, and isolation complexity before validating the core integration.

## Managed and External Modes

The eventual configuration could support:

```yaml
adapters:
  chimera:
    transport: native
    lifecycle: managed # managed | external
    endpoint: http://127.0.0.1:4317
```

### Managed

Legato starts, supervises, and stops the Chimera server.

This should be the default for local task and swarm execution.

### External

Legato connects to an existing Chimera server.

This is useful for:

- development and debugging;
- containerized Chimera instances;
- a remote worker that owns the server lifecycle;
- sharing one server across several sessions; and
- deployments where a service manager owns Chimera.

External mode requires clear authentication, trust, and session-ownership
rules before it is exposed beyond localhost.

## Event Translation

Chimera events map cleanly onto Legato's desired run model.

| Chimera event | Legato behavior |
|---|---|
| `session_started` | Mark the execution as running |
| `user_message` | Record a conversation input |
| `assistant_text_delta` | Stream live conversation output |
| `assistant_text_done` | Persist the completed response |
| `tool_call_start` | Show the current action and mark working |
| `tool_call_result` | Record the structured result and duration |
| `tool_call_error` | Record a structured tool failure |
| `permission_request` | Create an attention item and mark waiting |
| `permission_resolved` | Resolve the attention item and mark working |
| `permission_timeout` | Expire the attention item and apply policy |
| `task_list_updated` | Update structured agent progress |
| `usage_updated` | Record context and usage metrics |
| `subagent_spawned` | Create or associate a nested run |
| `subagent_event` | Route a nested agent event |
| `subagent_finished` | Complete the nested run |
| `step_finished` | Record an agent step checkpoint |
| `run_finished` | Complete, fail, time out, or cancel the execution |
| `compaction_started` | Show compaction activity |
| `compaction_finished` | Record the context reduction |
| `compaction_failed` | Record a non-terminal compaction failure |
| `mode_changed` | Update effective mode and model metadata |
| `model_changed` | Update the effective model |

Legato should retain Chimera-specific payloads where useful while normalizing
the common lifecycle into general run events.

For example:

```go
type RunEvent struct {
    ID          string
    RunID       string
    ExecutionID string
    Kind        RunEventKind
    Source      string
    Payload     json.RawMessage
    CreatedAt   time.Time
}
```

`Source` could be `chimera`, while `Kind` uses general values such as:

- `run.started`;
- `message.delta`;
- `message.completed`;
- `tool.started`;
- `tool.completed`;
- `tool.failed`;
- `approval.requested`;
- `approval.resolved`;
- `progress.updated`;
- `usage.updated`;
- `run.completed`; and
- `run.failed`.

## User Experience

A native Chimera integration could make the agent panel structured rather than
terminal-centric.

For example:

```text
Fix login redirect
Chimera · working · 12m

Current action
  Running Go integration tests

Plan
  ✓ Reproduce redirect loop
  ✓ Locate cookie-path mismatch
  ⟳ Implement and test fix
  ○ Prepare summary

Usage
  28,400 / 200,000 context tokens
  14 tool calls

Permissions
  Waiting: run docker compose up
  [Allow once] [Allow session] [Deny]
```

Reliable native controls could include:

- interrupt the current turn;
- send a follow-up;
- inject a message at the next clean step boundary;
- allow or deny a permission;
- remember a permission for the session or project;
- change model;
- change Chimera mode;
- compact context;
- resume a session;
- fork from a checkpoint;
- rewind the session and workspace;
- inspect child agents; and
- inspect exact run termination reasons.

The terminal view can remain available as a diagnostic or compatibility view,
but it no longer needs to be the authoritative representation of the run.

## Attention Inbox Improvements

The native runner would substantially improve Legato's attention model.

Instead of receiving only a generic hook update that marks an agent as
`waiting`, Legato could persist:

- the permission request ID;
- the tool requesting permission;
- the target environment;
- the exact command;
- the reason supplied by Chimera;
- when the request was created;
- when it will time out; and
- which user or policy resolved it.

This enables safe approval actions from web and mobile without synthesizing
terminal keystrokes.

It also enables approval policies such as:

- automatically allow read-only tools;
- allow a known test command for the session;
- require user approval for network access;
- deny destructive commands;
- time out unattended permissions; and
- escalate particular requests through notifications.

## Session Resume and Handoff

Chimera's persisted sessions and event cursors make it a strong candidate for
Legato's first durable execution integration.

Legato should persist at least:

```text
runner_kind          chimera-native
runner_instance_url  http://127.0.0.1:...
runner_pid            ...
external_session_id  Chimera session ID
last_event_id         latest consumed SSE event
runner_managed        true or false
```

On Legato restart:

1. Check whether the managed process or external endpoint is reachable.
2. Inspect the Chimera session.
3. Subscribe from `last_event_id`.
4. Reconcile Legato's execution state with the Chimera session state.
5. Resume consuming events.
6. If the process is gone but the session persists, attempt or offer resume in
   a new Chimera server.
7. If recovery is impossible, mark the execution as lost rather than silently
   treating it as complete.

This behavior would validate many of the recovery semantics required by future
remote workers.

## Relationship to the General Runner Model

The integration should not add every Chimera operation directly to
`AgentService`.

The general runner boundary should contain only operations common to agent
execution:

```go
type AgentRunner interface {
    Start(ctx context.Context, spec RunSpec) (RunHandle, error)
    Stop(ctx context.Context, runID string) error
    Send(ctx context.Context, runID string, input AgentInput) error
    Subscribe(
        ctx context.Context,
        runID string,
        cursor EventCursor,
    ) (EventStream, error)
    Inspect(ctx context.Context, runID string) (RunStatus, error)
}
```

Richer features should be exposed through optional capability interfaces:

```go
type PermissionRunner interface {
    ResolvePermission(
        ctx context.Context,
        runID string,
        requestID string,
        decision PermissionDecision,
    ) error
}

type ResumableRunner interface {
    Resume(ctx context.Context, runID string) error
}

type ForkableRunner interface {
    Fork(ctx context.Context, runID string, checkpoint int) (RunHandle, error)
    Rewind(ctx context.Context, runID string, checkpoint int) error
}

type ConfigurableRunner interface {
    SetModel(ctx context.Context, runID string, model string) error
    SetMode(ctx context.Context, runID string, mode string) error
}
```

This approach prevents Chimera's richer interface from forcing artificial
implementations onto:

- Claude Code;
- Codex;
- OpenCode;
- arbitrary shell sessions;
- legacy tmux execution; and
- future remote runners.

The API and UI can query runner capabilities and display only supported
actions.

## Role of Tmux After Native Integration

Tmux should remain supported for:

- harnesses without structured APIs;
- arbitrary interactive shells;
- direct user attachment;
- debugging native integrations;
- compatibility with existing workflows;
- visual access to a harness's native TUI; and
- fallback when the Chimera protocol is unavailable or incompatible.

For Chimera, however, tmux should become the fallback transport rather than the
preferred orchestration path.

## Implementation Sequence

### Phase 1: Protocol spike

- Start `chimera serve --machine-handshake` from a Go test or development
  command.
- Parse the handshake.
- Implement SSE parsing and session inspection.
- Submit one prompt and consume events through `run_finished`.
- Validate interrupt and permission resolution.
- Document the exact protocol subset used.

Exit criterion:

> A Go process can manage a complete Chimera run without tmux or terminal
> scraping.

### Phase 2: General runner boundary

- Define normalized run specifications and events.
- Introduce an `AgentRunner` interface.
- Wrap current behavior in `TmuxRunner`.
- Keep existing TUI, web, and swarm behavior unchanged.
- Add runner capability discovery.

Exit criterion:

> Existing agents continue working through `TmuxRunner`, while agent services
> no longer directly define execution in tmux-specific terms.

### Phase 3: Minimal Chimera native runner

- Add the Go Chimera client.
- Add managed process lifecycle.
- Create and resume sessions.
- Send initial prompts and follow-up messages.
- Stream structured output.
- Handle explicit completion and interruption.
- Persist Chimera session and event-cursor metadata.

Exit criterion:

> A normal task and a swarm worker can use `transport: native` and complete
> without a tmux session.

### Phase 4: Structured attention and progress

- Map permission requests into the Legato attention inbox.
- Add allow and deny actions to web and mobile.
- Map task-list updates into progress UI.
- Persist tool-call summaries and durations.
- Record context usage and terminal reasons.
- Remove the requirement for Chimera activity hooks in native mode.

Exit criterion:

> Legato knows exactly why a Chimera agent is working, waiting, or finished
> without inspecting terminal output.

### Phase 5: Recovery and advanced capabilities

- Reconnect SSE streams from the last event ID.
- Reconcile sessions after Legato restart.
- Add model and mode controls.
- Add compaction controls.
- Add fork and rewind workflows.
- Display nested Chimera subagents.
- Support externally managed Chimera servers.

Exit criterion:

> A native Chimera run survives temporary connection loss and Legato server
> restart without losing its history or control surface.

## Testing Strategy

The integration should have several test layers.

### Go client unit tests

Use an in-process HTTP test server to verify:

- request paths and payloads;
- non-success response handling;
- SSE parsing;
- event IDs;
- reconnect cursors;
- permission conflicts;
- cancellation; and
- malformed event behavior.

### Runner unit tests

Use a fake Chimera client and fake process supervisor to verify:

- lifecycle transitions;
- event translation;
- terminal-reason mapping;
- managed cleanup;
- reconnection behavior;
- persistence updates; and
- idempotent cancellation.

### Protocol contract tests

Run against a real `chimera serve` process to verify:

- handshake parsing;
- session creation and inspection;
- message submission;
- event subscription;
- permission resolution;
- interrupt behavior;
- resume behavior; and
- run completion.

These tests should be version-aware and clearly distinguish protocol
incompatibility from an agent or model failure.

### Existing compatibility tests

Retain tests for the tmux-based Chimera adapter so users can select or fall
back to `transport: tmux`.

## Risks and Mitigations

### Protocol drift

Chimera's TypeScript client and server may evolve without a formally versioned
external protocol.

Mitigations:

- check Chimera instance version at connection time;
- keep the Go client small;
- add contract tests against supported Chimera versions;
- tolerate unknown event types;
- fail clearly on incompatible required fields; and
- pursue an OpenAPI document or shared protocol test suite.

### Process supervision

Managed Chimera servers may exit unexpectedly or leave stale processes.

Mitigations:

- persist PID and endpoint metadata;
- use root lifecycle contexts;
- collect exit status;
- reconcile at startup;
- apply bounded graceful shutdown followed by termination; and
- distinguish agent failure from runner-process failure.

### Port allocation

One server per run needs safe ephemeral port allocation.

Mitigations:

- let Chimera bind an ephemeral localhost port;
- rely on the machine handshake for the selected URL;
- never derive ports from task IDs; and
- keep endpoints loopback-only in managed local mode.

### Double persistence

Both Chimera and Legato will retain aspects of session history.

Mitigation:

- Chimera remains authoritative for its conversation and internal session
  reconstruction;
- Legato stores normalized orchestration events, audit history, and artifact
  references;
- Legato stores external session IDs and cursors rather than duplicating every
  internal Chimera object; and
- reconciliation rules explicitly define which state wins.

### Feature coupling

The Legato UI could accidentally become dependent on Chimera-only features.

Mitigation:

- expose optional capabilities;
- keep common run views harness-neutral;
- label Chimera-specific actions;
- provide graceful fallback views; and
- avoid adding Chimera concepts directly to generic task records.

### Authentication and exposure

An externally reachable Chimera server would be a powerful code-execution
surface.

Mitigation:

- keep managed mode loopback-only;
- do not expose its endpoint through the Legato API;
- require authentication and transport security before supporting remote
  external endpoints; and
- prefer running Chimera inside the same trusted worker environment as the
  Legato runner.

## Expected Benefits

A native Chimera runner would:

- materially improve Chimera's Legato experience;
- remove terminal scraping and keystroke fragility;
- eliminate Chimera activity hooks in native mode;
- provide exact permission and lifecycle state;
- enable dependable web and mobile controls;
- expose usage, progress, tools, and subagents;
- support replay and restart recovery;
- validate Legato's normalized event model;
- establish the first non-tmux runner;
- inform later Docker and remote-worker implementations; and
- make Chimera the strongest first-class harness without coupling the entire
  platform to Chimera.

## Overall Verdict

Proceed.

The most valuable framing is:

> Chimera Native Runner is the proving ground for Legato's durable and
> distributed agent-execution architecture.

It should not be treated as a one-off Chimera shortcut. The implementation
should establish the runner, event, capability, lifecycle, and recovery
boundaries that future agent harnesses and cloud workers can share.
