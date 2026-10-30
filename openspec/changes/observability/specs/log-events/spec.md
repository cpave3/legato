## ADDED Requirements

### Requirement: What is never logged
Legato SHALL NOT log secret values, or what people and agents write: prompts, messages, terminal output, conversations, task titles and descriptions, plan and review text, or file contents. These SHALL be logged as sizes (such as `prompt_bytes`) and IDs only. Requests SHALL be logged by route pattern, never by full URL or query string. Request and response bodies SHALL NOT be logged.

#### Scenario: Message to an agent
- **WHEN** the person sends an agent a message
- **THEN** `agent.message_queued` SHALL carry `task_id`, `source` and `bytes`, and SHALL NOT carry the text

#### Scenario: WebSocket with a token in its URL
- **WHEN** a client connects to `/ws?token=…`
- **THEN** `ws.connected` SHALL carry the route `/ws` and SHALL NOT carry the query string

### Requirement: Start and stop
A host process SHALL log `legato.started` at `info`, with:
- the version and build ID;
- the runtime (`serve`, `tui`);
- the listeners and whether TLS is on;
- the data folder;
- the features on (sandbox, credentials and their stores by scheme, MCP tools, integrations);
- the configuration file's path.

It SHALL log `legato.stopping` with the reason (signal, error), and `legato.stopped`. At `debug`, it SHALL log `config.loaded` with the settings, secrets and references shown as the reference's scheme only.

#### Scenario: Start under systemd
- **WHEN** `legato serve` starts
- **THEN** one `legato.started` line SHALL say the version, the listeners and which features are on

### Requirement: Requests and connections
Every API request SHALL be logged as `http.request` at `info`, with:
- the method and route pattern;
- the status and duration;
- `request_id`, `client`, the remote address and a shortened user agent.

Responses with status 500 or above SHALL be logged at `error`; 401 and 403 at `warn`. WebSocket connections SHALL be logged as `ws.connected` and `ws.disconnected` (with duration and reason). Refused authentication SHALL be logged as `auth.refused` at `warn`, with the remote address and the reason.

#### Scenario: Refused token
- **WHEN** a request presents a wrong auth token
- **THEN** Legato SHALL log `auth.refused` at `warn` with the remote address, and SHALL NOT log the presented token

### Requirement: Devices and pairing
Legato SHALL log at `info`:
- `pairing.started`, `pairing.succeeded` and `pairing.failed` (with the reason and attempts left);
- `device.added`, `device.revoked` and `device.reset`;
- `approval.signature_verified` and `approval.signature_refused` (at `warn`).

#### Scenario: Failed pairing attempt
- **WHEN** a phone sends a wrong phrase
- **THEN** `pairing.failed` SHALL be logged with the attempts left and SHALL NOT include the phrase

### Requirement: Agents
Legato SHALL log at `info`:
- **`agent.spawned`:** `task_id`, `execution_id`, `agent_kind`, `sandboxed`, the working folder, the resource names, who asked (`client` or the workflow/swarm/Argus that did), and `prompt_bytes`.
- **`agent.spawn_failed`:** at `error`, with the reason.
- **`agent.restarted`, `agent.resumed`, `agent.killed`:** each with who asked.
- **`agent.ended`:** `reason`, the exit code when known, and the seconds it ran.
- **`agent.activity_changed`:** the previous and new activity.
- **`agent.bridge_bound` and `agent.bridge_lost`.**
- **`agent.message_queued`, `agent.message_delivered` and `agent.message_failed`.**
- **`sandbox.refused`:** at `warn`, with the reason (bwrap missing, mount refused).

At `debug`, `agent.launch` SHALL log the launch command with secrets and the prompt left out.

#### Scenario: Agent ends
- **WHEN** an agent's session ends after 40 minutes
- **THEN** `agent.ended` SHALL carry `task_id`, `execution_id`, `agent_kind`, `reason` and `seconds` near 2400

### Requirement: Approvals and credentials
Legato SHALL log at `info`:
- **Approvals:** `approval.requested` (`approval_id`, source, task); `approval.decided` (outcome, `via`, the device when signed, seconds waited); `approval.expired`.
- **Credentials:**
  - `credential.requested`, `credential.granted` (the mode), `credential.denied`;
  - `credential.used`, with the operation, the resource and the audit sequence number, so the audit row can be found;
  - `credential.refused`, at `warn`;
  - `credential.grant_ended`.
- **The proxy:** `proxy.request`, with the profile, the route, the status and the upstream duration.
- **SSH:** `ssh.login`, with the host name and the result.

At `debug`, `credential.rule_matched` SHALL name the rule that decided a use.

#### Scenario: A refused push
- **WHEN** the proxy refuses a push to a branch no grant covers
- **THEN** `credential.refused` SHALL be logged at `warn` with `task_id`, the credential, the operation and the resource, and no token

### Requirement: Workflows, swarms and Attention
Legato SHALL log:
- **Workflow runs:** `workflow.run_started` and `workflow.run_finished`, with the result and seconds.
- **Steps:** `workflow.step_started`, `workflow.step_completed`, `workflow.step_failed` (at `warn`, with the reason) and `workflow.step_retried`.
- **Triggers:** `workflow.trigger_fired` and `workflow.trigger_skipped`, with the reason.
- **Swarms:** `swarm.started`, `swarm.dispatched`, `swarm.subtask_changed` and `swarm.finished`.
- **Attention:** `attention.item_opened` and `attention.item_resolved`, with the producer, at `debug`.

#### Scenario: Step failure
- **WHEN** a workflow step fails
- **THEN** `workflow.step_failed` SHALL carry `workflow_run_id`, the step ID and type, the attempt and the reason

### Requirement: Sync, broker and background work
Legato SHALL log:
- **Sync:** `sync.finished` for each sync, with the provider, the duration and the counts of changed items; `sync.failed` at `warn`, with the kind (`auth`, `rate_limit`, `offline`, `other`); `github.rate_limit_low` at `warn`, when fewer than 10% of requests remain.
- **The broker:** `broker.call`, at `debug`, with the operation and the result; `broker.refused` at `warn`, with the operation and the code.
- **Hosting:** `channels.host_taken` and `channels.host_lost`; `broker.host_taken`.
- **Background work:** `reconcile.changed` when a reconcile changed something, with what changed; at `debug` when nothing changed.

#### Scenario: GitHub rate limit
- **WHEN** a GitHub response says 400 of 5000 requests remain
- **THEN** `github.rate_limit_low` SHALL be logged at `warn` with the remaining count and the reset time
