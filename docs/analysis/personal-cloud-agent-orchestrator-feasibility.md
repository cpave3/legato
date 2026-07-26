# Personal Cloud Agent Orchestrator Feasibility

## Summary

Evolving Legato into a headless server and a personal cloud-agent
orchestrator is feasible.

Legato already has much of the required control plane:

- a real `legato serve` process;
- an HTTP and WebSocket API;
- an embedded web dashboard and PWA;
- bearer authentication and TLS support;
- conductor/worker swarm orchestration;
- persisted plans, reviews, artifacts, agent sessions, and swarm events;
- multi-step plans and human approval gates;
- multiple agent harness adapters;
- terminal streaming and remote agent interaction; and
- SQLite-backed task and execution history.

The main architectural challenge is therefore not adding a headless mode. It is
replacing local-machine assumptions—especially tmux, local filesystems, local
Git checkouts, and in-process events—with durable abstractions suitable for
remote and isolated execution.

The recommended initial product is a self-hostable, single-user personal agent
control plane. One Legato server would coordinate agents running on a laptop,
home server, VPS, or small Kubernetes cluster.

This is a more achievable and differentiated goal than attempting immediate
feature parity with a multi-tenant enterprise platform such as Warp Oz.

## Feasibility Assessment

| Goal | Feasibility | Character of work |
|---|---|---|
| Hardened headless personal server | High | Incremental |
| Single-host isolated agent execution | High | Significant but contained |
| Personal multi-machine agent orchestrator | Medium-high | Architectural and distributed |
| Oz-scale multi-tenant platform | Low as an initial goal | Different product category |

The work should be approached as a sequence of compatible uplifts rather than a
rewrite.

## Current Legato Capabilities

Legato already implements many capabilities commonly associated with agent
orchestration platforms.

### Headless and remote access

`legato serve` currently provides:

- HTTP APIs;
- WebSocket communication;
- embedded React web assets;
- TLS configuration;
- bearer-token authentication;
- IPC integration;
- signal handling;
- agent, swarm, plan, review, artifact, and task endpoints; and
- event forwarding to connected clients.

### Agent orchestration

The swarm system already provides:

- conductor-driven planning;
- worker dispatch;
- parallel execution;
- multi-step plans;
- explicit step advancement;
- human plan approval;
- progress, question, and completion reporting;
- worker steering and broadcast messages;
- cancellation and cleanup;
- agent scope declarations and conflict checks;
- adapter and model-tier selection; and
- persisted pending plans and swarm events.

### Agent harnesses

Legato supports or has designs for multiple agent harnesses, including:

- Claude Code;
- Codex;
- Chimera; and
- OpenCode.

This multi-harness orientation is an important foundation. The orchestrator
should remain independent of any particular agent CLI or model provider.

### Visibility and interaction

The current web and mobile-oriented interfaces already support much of the
management experience:

- listing active agents;
- streaming terminal output;
- sending text and keys;
- detecting prompts requiring attention;
- reviewing plans;
- viewing swarm progress;
- inspecting artifacts; and
- performing review workflows.

### Persistence

SQLite currently stores tasks, agent sessions, state intervals, swarm state,
plans, reviews, artifacts, and related activity. It is suitable for an initial
single-server personal control plane.

## Comparison with Warp Oz

Warp describes Oz as a programmable cloud-agent orchestration platform with:

- parallel cloud agents;
- multiple agent harnesses;
- sandboxed environments;
- CLI, API, web, mobile, and integration-driven triggers;
- schedules and recurring workflows;
- agent steering and management;
- audit trails and shareable runs;
- persistent memory;
- hosted and self-hosted execution; and
- distributed workers using outbound connectivity.

Legato already overlaps with Oz in several areas:

- multi-harness support;
- parallel conductor/worker orchestration;
- task, plan, and review management;
- web, mobile, and terminal control surfaces;
- agent steering;
- persistent execution records; and
- self-hosted operation.

The largest gaps are:

- isolated execution environments;
- remote worker registration and scheduling;
- durable run leasing and recovery;
- triggers and schedules;
- environment and repository provisioning;
- credential and secrets management;
- structured audit logs;
- cost and resource controls;
- machine-to-machine handoff; and
- cross-run memory.

Relevant external references:

- [Introducing Oz: the orchestration platform for cloud agents](https://www.warp.dev/blog/oz-orchestration-platform-cloud-agents)
- [A single pane of glass for managing all of your cloud agents](https://www.warp.dev/blog/multi-harness-cloud-agent-orchestration)
- [Warp architecture and deployment](https://docs.warp.dev/enterprise/enterprise-features/architecture-and-deployment)

## Current Architectural Constraint

The current architecture is approximately:

```text
HTTP / Web / TUI
        │
   service layer
        │
   tmux manager
        │
 local agent CLI
        │
local filesystem and Git checkout
        │
   local SQLite
```

At the service boundary, agent operations are currently described in tmux
terms:

- spawn a session;
- kill a session;
- capture a pane;
- attach to a pane;
- send keys;
- pipe output; and
- inspect tmux liveness.

This is appropriate for local interactive execution, but it makes the agent
session, execution environment, and tmux process effectively the same concept.

Cloud orchestration requires those concepts to be separated.

## Target Architecture

The target architecture should have an explicit control plane and one or more
execution planes:

```text
                         ┌─────────────────────┐
                         │ Local tmux runner   │
CLI / Web / Mobile       ├─────────────────────┤
          │              │ Direct runner      │
          ▼              ├─────────────────────┤
 Legato control plane ──►│ Docker runner      │
          │              ├─────────────────────┤
          │              │ Remote worker      │
          │              ├─────────────────────┤
          │              │ Kubernetes runner  │
          │              └─────────────────────┘
          ▼
 runs, events, leases, logs, artifacts,
 approvals, schedules, policies, and audit
```

The control plane should make orchestration decisions without knowing whether
an agent runs in tmux, a local process, a Docker container, or a Kubernetes pod.

## Runner Abstraction

The most important architectural change is to replace `TmuxManager` as the
service-level execution abstraction with a runner-oriented contract.

For example:

```go
type Runner interface {
    Start(ctx context.Context, spec RunSpec) (RunHandle, error)
    Stop(ctx context.Context, runID string) error
    Send(ctx context.Context, runID string, input AgentInput) error
    Logs(ctx context.Context, runID string, cursor LogCursor) (LogBatch, error)
    Inspect(ctx context.Context, runID string) (RunStatus, error)
}
```

Potential implementations:

- `TmuxRunner`: preserves all existing local behavior.
- `ProcessRunner`: directly executes an agent without requiring tmux.
- `DockerRunner`: runs an agent in an isolated local or server-side container.
- `RemoteRunner`: delegates execution to a connected Legato worker.
- `KubernetesRunner`: schedules an isolated execution in a Kubernetes pod.

Runner selection should be based on an execution profile or environment
reference attached to a run, not embedded inside swarm logic.

### Runner capabilities

Workers and runners should declare capabilities such as:

- supported operating systems and architectures;
- available agent harnesses;
- available model credentials;
- Docker or container support;
- maximum concurrency;
- CPU and memory capacity;
- repository or network access;
- trusted execution labels; and
- optional GPU or browser-computer-use support.

The scheduler can then match a run specification to a compatible runner.

## Control Plane and Worker Roles

### `legato server`

The server should become the durable control plane responsible for:

- API, WebSocket, web, and mobile connectivity;
- authentication and worker enrollment;
- task and swarm state;
- dispatch and scheduling decisions;
- run and execution records;
- approval inboxes;
- triggers and schedules;
- audit events;
- artifact metadata;
- secrets references;
- cost and usage accounting; and
- worker health and capacity.

The server should not assume that:

- a repository exists on its filesystem;
- an agent CLI is installed locally;
- tmux is installed;
- the server working directory is meaningful to an agent; or
- the server and execution environment share a filesystem.

### `legato worker`

A worker daemon should:

- connect outbound to the server;
- authenticate and register its capabilities;
- lease queued executions;
- heartbeat and renew execution leases;
- prepare an isolated workspace;
- clone or mount repositories;
- obtain scoped credentials;
- start the selected agent harness;
- stream structured events and logs;
- accept user or conductor input;
- upload patches, artifacts, and results;
- report resource usage; and
- clean up the environment.

Outbound connectivity is particularly suitable for a personal system because a
laptop, workstation, or home server can join the fleet without exposing an
inbound port.

## Domain Model

Cloud orchestration should separate at least four concepts.

### Task

The desired unit of work. This remains close to Legato's existing task model.

### Agent run

One logical attempt to complete a task or subtask. A run contains:

- prompt and input context;
- selected harness and model tier;
- environment requirements;
- lifecycle state;
- approvals;
- outputs and artifacts;
- cost and timing data; and
- one or more execution attempts.

### Execution

A particular allocation of a run to a worker. Retries and rescheduling create
new executions beneath the same logical run.

An execution contains:

- worker and runner identity;
- lease ownership and expiry;
- environment identity;
- start and finish timestamps;
- exit status;
- resource usage; and
- failure or termination reason.

### Conversation

Structured messages and activity associated with the run:

- user and conductor messages;
- agent responses;
- tool calls;
- approval requests;
- progress events; and
- system-generated lifecycle events.

This structure allows a run to survive:

- server restarts;
- browser and mobile disconnections;
- worker disconnections;
- agent CLI crashes;
- retries;
- movement to another machine; and
- control-plane upgrades.

## Run Lifecycle

A possible initial lifecycle is:

```text
created → queued → leased → preparing → running
                              │           │
                              └─ failed   ├─ waiting_for_input
                                          ├─ succeeded
                                          ├─ failed
                                          └─ cancelled
```

Important rules:

- Lease acquisition must be transactional.
- A worker must renew its lease with heartbeats.
- Expired leases can be reconciled and requeued according to policy.
- Retries create new execution attempts instead of erasing history.
- Run creation should accept an idempotency key.
- Terminal states should be immutable apart from administrative annotation.
- Cancellation must be safe to repeat.

## Durable Events

The existing in-memory event bus is appropriate for best-effort UI refresh
notifications, but it should not be the source of truth for distributed
execution.

Slow subscribers currently may miss events, and events disappear on process
restart. Remote workers and audit history require a durable event stream.

An initial SQLite `run_events` table could contain:

- monotonically increasing sequence;
- run ID;
- execution ID;
- event kind;
- structured payload;
- origin;
- timestamp; and
- optional causation and correlation IDs.

WebSocket clients and workers can consume events using sequence cursors. If a
connection drops, the consumer resumes from its last acknowledged sequence.

The in-memory bus can continue to act as a low-latency notification mechanism,
with durable tables remaining authoritative.

## Headless Server Hardening

Although `legato serve` exists, several changes would make it a robust daemon:

- move duplicated TUI and server wiring into a shared application runtime;
- derive service lifecycles from a root signal context;
- avoid `context.Background()` for request and process lifecycles;
- add separate liveness and readiness endpoints;
- add structured logging;
- add graceful shutdown for streams and event consumers;
- recover active runs and expired leases at startup;
- remove dependence on the server's startup working directory;
- version externally consumed APIs;
- add idempotency keys to mutating external operations;
- add database backup and restore procedures;
- add metrics for queue depth, active runs, failures, and worker health; and
- provide documented launchd, systemd, and Docker service configurations.

## Structured Agent Communication

Raw terminal streaming should remain available as an escape hatch, but it
should not be the primary orchestration protocol.

Depending on terminal scraping and `send-keys` creates several problems:

- prompts are difficult to identify reliably;
- harness UI changes can break detection;
- messages have weak delivery guarantees;
- acknowledgements are unavailable;
- structured tool activity is lost; and
- replay and audit are difficult.

Where a harness exposes hooks, JSON events, an SDK, or an API, the runner should
translate those into a common Legato event schema.

The orchestration boundary should use structured operations such as:

- send message;
- answer question;
- approve or deny action;
- cancel run;
- request status;
- publish progress;
- publish artifact; and
- report completion.

Terminal I/O can be retained for unsupported harnesses and interactive
debugging.

## Isolated Execution Environments

Docker is the recommended first cloud-capable execution backend.

A `DockerRunner` should support:

- a fresh per-run workspace;
- repository clone or controlled volume mount;
- configurable base images;
- CPU and memory limits;
- execution timeouts;
- optional read-only root filesystem;
- scoped environment variables and secrets;
- configurable network egress;
- artifact and patch collection;
- deterministic cleanup; and
- retention policies for failed environments.

Environment definitions should be reusable resources, for example:

```yaml
environments:
  - name: go-default
    runner: docker
    image: ghcr.io/example/legato-go-agent:latest
    cpu: 4
    memory: 8Gi
    timeout: 2h
    network_policy: restricted
    setup:
      - go mod download
```

The environment definition should describe execution requirements without
being coupled to a particular swarm plan.

## Repository and Artifact Handling

Remote runs cannot rely on a developer's existing working copy.

The control plane needs explicit repository inputs:

- repository URL;
- revision or base branch;
- authentication reference;
- optional sparse checkout;
- optional multi-repository workspace;
- desired output branch; and
- patch, commit, or pull-request policy.

Run outputs should be first-class artifacts:

- Git patches;
- commits and branch references;
- pull-request links;
- logs;
- test and lint reports;
- screenshots;
- generated files;
- review findings; and
- structured return values.

Artifacts should be stored outside WebSocket buffers. Initially this can be a
local filesystem content store with metadata in SQLite. An S3-compatible object
store can be added when remote deployments need it.

## Security Model

Security is the largest product risk. A remotely triggered coding agent is
remote code execution by design.

A credible initial security model should include:

- isolated execution by default for remote runs;
- no arbitrary host execution unless explicitly enabled;
- repository allowlists;
- per-environment trust levels;
- scoped, short-lived credentials where possible;
- secret references rather than stored secret values;
- redaction of secrets from logs and events;
- configurable network egress;
- immutable audit events;
- explicit approval policies;
- CPU, memory, duration, concurrency, and spend limits;
- authenticated worker enrollment;
- worker identity rotation and revocation;
- strict WebSocket origin validation;
- CSRF protection for browser-authenticated operations; and
- rate limiting for externally exposed APIs.

The current permissive WebSocket origin setting is appropriate only for trusted
local-network use and must be tightened before internet-facing deployment.

## Persistence Strategy

SQLite remains a good fit for the first version of a personal control plane.

Recommended constraints:

- one authoritative server process;
- WAL mode;
- short transactions;
- transactional run leasing;
- durable append-only events;
- indexed run and lease queries;
- periodic online backups; and
- schema migration tests.

PostgreSQL should be introduced only when Legato requires:

- multiple active control-plane replicas;
- high availability;
- many concurrent dispatchers;
- multi-user tenant isolation; or
- operational scale beyond a personal or small-team system.

Moving to PostgreSQL early would add deployment complexity without solving the
main runner and execution-model problems.

## Delivery Roadmap

### Phase 1: Headless runtime hardening

Keep execution local and tmux-based.

Deliver:

- canonical long-running `legato server` mode;
- shared application wiring for TUI and server;
- durable run and event records;
- a run-oriented API;
- restart reconciliation;
- liveness and readiness checks;
- structured logs;
- API versioning; and
- basic audit history.

Success criterion:

> Restarting the server does not lose knowledge of active, waiting, or recently
> terminated agents.

### Phase 2: Runner abstraction

Move tmux behind a general execution boundary.

Deliver:

- `Runner` interface;
- `TmuxRunner`;
- `ProcessRunner`;
- runner capability discovery;
- run leases and heartbeats;
- cursor-based logs;
- structured agent input; and
- execution-attempt persistence.

Success criterion:

> Swarm orchestration no longer imports or conceptually depends on tmux.

### Phase 3: Single-host isolated execution

Add `DockerRunner`.

Deliver:

- repository checkout;
- per-run container and workspace;
- resource and duration limits;
- network policy;
- credential injection;
- artifact and patch collection;
- environment templates; and
- cleanup and retention policies.

Success criterion:

> Legato running on a VPS can execute several isolated Codex, Claude Code, or
> other supported harness jobs without a desktop or tmux session.

### Phase 4: Remote personal workers

Add `legato worker connect`.

Deliver:

- outbound authenticated worker connections;
- worker enrollment and revocation;
- capability labels;
- lease-based scheduling;
- disconnect recovery;
- capacity and concurrency reporting;
- routing by platform, harness, repository access, and trust level; and
- unified visibility for local and remote workers.

Success criterion:

> One Legato server can dispatch work between a laptop, workstation, and cloud
> VM and recover safely when one becomes unavailable.

### Phase 5: Personal automation platform

Add the workflow layer that makes cloud agents useful without manual dispatch.

Deliver:

- cron schedules;
- webhooks;
- GitHub, Jira, and Linear triggers;
- reusable workflows and skills;
- reusable environment definitions;
- notifications and approval policies;
- budgets and concurrency limits;
- run sharing;
- persistent cross-run memory; and
- an API/SDK-first run-creation surface.

Success criterion:

> A scheduled or webhook-triggered agent can autonomously produce a reviewable
> pull request while remaining visible, auditable, and steerable.

## Recommended Product Positioning

Legato should not initially position itself as an open-source Oz replacement.
That framing implies:

- enterprise multi-tenancy;
- hosted inference and compute;
- billing;
- organization-wide governance;
- large-scale worker fleets;
- high availability; and
- enterprise integrations and support.

A clearer initial position is:

> Legato is a self-hosted personal control plane for coding agents. Run Claude
> Code, Codex, OpenCode, Chimera, or other harnesses locally or on your own
> cloud machines, coordinate them as swarms, and supervise everything from the
> web, mobile, or terminal.

Potential differentiation:

- personal and local-first;
- bring your own harness, model, credentials, and compute;
- human-in-the-loop orchestration;
- strong task, plan, artifact, and review workflows;
- scales from one laptop to a small personal fleet;
- no mandatory hosted control plane;
- Git-native outputs; and
- transparent, inspectable execution history.

## Major Risks

### Execution security

Remote agent execution can expose source code, credentials, and infrastructure.
Isolation and secrets design must precede broad trigger integrations.

### False durability

Displaying an in-memory agent as active is not enough. Leases, events, and
execution attempts must be persisted and reconciled.

### Harness compatibility

Terminal scraping is brittle. Structured harness adapters should be preferred
wherever possible.

### Premature infrastructure

Kubernetes, PostgreSQL, and a general workflow language could consume
substantial effort before the core personal-worker experience is proven.

### Unbounded product scope

Cloud execution, CI, issue automation, memory, code review, and team governance
can each become a product. The initial boundary should remain a single-user
control plane with a small trusted worker fleet.

### Cost and runaway agents

Schedules and parallel swarms can create unexpected model and compute costs.
Budgets, timeouts, concurrency limits, and cancellation must be core concepts.

## Architectural Principles

The evolution should follow these principles:

1. Preserve local workflows through `TmuxRunner`.
2. Keep orchestration independent of execution technology.
3. Make durable state authoritative and WebSockets a projection.
4. Treat every retry as a new execution attempt.
5. Prefer outbound worker connectivity.
6. Use structured agent events where available.
7. Isolate remotely triggered work by default.
8. Keep SQLite until scale proves it insufficient.
9. Design APIs before adding many trigger integrations.
10. Optimize first for a single user and a small trusted fleet.

## Recommended Next Design Artifact

The next concrete step should be an OpenSpec proposal for **distributed agent
execution**.

It should define:

- `Run`;
- `Execution`;
- `Runner`;
- `Worker`;
- runner capabilities;
- run and execution lifecycle states;
- durable run events;
- log cursors;
- execution leases and heartbeats;
- retry and reconciliation rules;
- structured agent input;
- initial `TmuxRunner` compatibility; and
- migration of `AgentService` and `SwarmService` onto the new boundary.

The proposal should deliberately avoid requiring Docker, Kubernetes, or
PostgreSQL in its first implementation. Its purpose is to establish the domain
and service boundaries that allow those execution backends to be added safely.

## Overall Recommendation

Proceed with the exploration.

The work naturally divides into:

- a moderate headless-server hardening project;
- a substantial but contained runner and Docker project;
- a larger distributed-worker project; and
- an ongoing automation and personal-agent-platform product program.

The most valuable early milestone is not running hundreds of agents. It is
proving that one durable Legato server can safely run and supervise agents both
locally and on one remote machine without changing swarm, plan, review, or task
semantics.
