## ADDED Requirements

### Requirement: The metrics listener
When `observability.metrics.listen` is set, a host process (`legato serve` or the TUI) SHALL serve Prometheus metrics in the text exposition format at `/metrics` on that address, separate from the web server. When it is empty (the default), no listener SHALL be opened. When `observability.metrics.token_file` is set, the endpoint SHALL require `Authorization: Bearer <token>`, reading the file again on each request, and SHALL refuse a file that other users can read. A listener on an address other than loopback without a token SHALL log `metrics.unprotected` at `warn`. A host that cannot bind the address SHALL log `metrics.listen_failed` and carry on. Short-lived processes SHALL NOT serve metrics.

#### Scenario: Scrape with the token
- **WHEN** Prometheus scrapes `/metrics` with the token from `token_file`
- **THEN** it SHALL get the metrics with status 200

#### Scenario: Scrape without the token
- **WHEN** a request to `/metrics` carries no token or a wrong one and a token is configured
- **THEN** it SHALL get 401 and no metrics

#### Scenario: Off by default
- **WHEN** `observability.metrics.listen` is not set
- **THEN** no metrics port SHALL be open

### Requirement: Agent metrics
The endpoint SHALL export these gauges, read from the store at each scrape:
- `legato_agents{state, agent_kind, sandboxed}`;
- `legato_agent_info{task_id, execution_id, agent_kind, state, sandboxed, role}` (value 1, live agents only);
- `legato_agent_running_seconds{task_id, agent_kind}` (live agents only);
- `legato_native_bridges{agent_kind}`.

It SHALL export `legato_agent_state_seconds_total{state, agent_kind}`, computed from the recorded state intervals.

It SHALL export these, counted when they happen:
- `legato_agent_spawns_total{agent_kind, sandboxed, result}`;
- `legato_agent_ends_total{agent_kind, reason}`;
- `legato_agent_session_seconds{agent_kind}` (a histogram);
- `legato_agent_messages_total{source, result}`.

No label SHALL hold a task title, prompt, path or branch.

#### Scenario: Agents by state
- **WHEN** two Claude Code agents are working and one Codex agent is waiting
- **THEN** the scrape SHALL show `legato_agents{state="working",agent_kind="claude-code"} 2` and `legato_agents{state="waiting",agent_kind="codex"} 1`

#### Scenario: An agent ends
- **WHEN** a live agent ends
- **THEN** its `legato_agent_info` and `legato_agent_running_seconds` series SHALL be absent from the next scrape, and `legato_agent_ends_total` SHALL increase

#### Scenario: Correct after a restart
- **WHEN** Legato restarts while three agents run
- **THEN** the first scrape after start SHALL show the three agents in `legato_agents`

### Requirement: Approval, credential, workflow, Attention and sync metrics
The endpoint SHALL export the metrics listed in `design.md` D6:
- **approvals:** pending, oldest pending, decisions by source, outcome and `via`, and the wait histogram;
- **credentials:** uses by credential, operation and result, active grants, proxy requests and upstream time by profile, SSH logins;
- **workflows:** runs by status, the run duration histogram, step failures by type;
- **swarms:** sub-tasks by status;
- **Attention:** items by producer and urgency;
- **sync:** duration and errors by provider and kind, `legato_github_rate_limit_remaining`.

#### Scenario: Approval decided on the phone
- **WHEN** the person allows a pending credential request with a signed approval on the phone
- **THEN** `legato_approval_decisions_total{source="credential",outcome="allow",via="signed"}` SHALL increase and `legato_approvals_pending{source="credential"}` SHALL decrease at the next scrape

### Requirement: Server and process metrics
The endpoint SHALL export:
- **HTTP:** `legato_http_requests_total{route, method, status_class}` and `legato_http_request_seconds{route, method}` (by route pattern);
- **WebSocket and broker:** `legato_websocket_clients`; `legato_broker_calls_total{op, result}`;
- **the build:** `legato_build_info{version, build_id, go_version}`, `legato_start_time_seconds`;
- **the database:** `legato_database_bytes`, `legato_database_busy_total`;
- **collectors:** `legato_collector_errors_total{collector}`;
- **the runtime:** Go runtime and process metrics.

A collector that fails or takes more than 2 seconds SHALL be left out of that scrape and counted in `legato_collector_errors_total`. The rest of the scrape SHALL succeed.

#### Scenario: Route pattern as label
- **WHEN** requests reach `/api/tasks/abc123/agent` and `/api/tasks/def456/agent`
- **THEN** both SHALL be counted under `route="/api/tasks/{id}/agent"`

#### Scenario: Slow collector
- **WHEN** the workflow collector's query takes longer than 2 seconds
- **THEN** the scrape SHALL return the other metrics, and `legato_collector_errors_total{collector="workflows"}` SHALL increase
