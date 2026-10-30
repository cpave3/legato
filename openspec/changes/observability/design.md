## Context

- **Logging today.**
  - 92 `log.Printf`/`log.Println` calls across `cmd/` and `internal/`, unstructured and without levels.
  - The TUI calls `log.SetOutput(io.Discard)` (`cmd/legato/main.go`), because stray writes corrupt Bubble Tea's alt screen, so its log is lost.
  - `legato serve` writes to standard error.
  - Short-lived processes (hooks, `legato mcp serve`, bridges, CLI commands) log to their own standard error. In the sandbox, much of their work runs in the host through the broker.
- **Metrics today:** none.
- **The target deployment** (`docs/cloud-agents.md`):
  - `legato serve` under systemd on a VM, so standard error goes to journald;
  - Grafana Alloy on the VM ships journald to Loki;
  - Prometheus on the same LAN scrapes the VM;
  - Grafana reads both;
  - node_exporter covers the host itself.
- **What Legato already records:**
  - `state_intervals` (time in each activity state);
  - `executions`;
  - approvals;
  - the credential audit log (hash-chained);
  - workflow runs;
  - Attention items.

  Metrics that describe current state are read from these, not counted again.

## Goals / Non-Goals

**Goals:**
- A person reading Loki after the fact can answer: what happened to this agent, who started or stopped it, why a spawn or a sync failed, who decided an approval and on which device, which requests came from where.
- Logs are structured, filterable by stable event names, and safe: no secret, and nothing people or agents wrote.
- Prometheus shows how many agents run in which state over time, how long they run, and the health of approvals, credentials, workflows, sync and the server.
- It works with the person's stack without Legato knowing about Loki: JSON to standard error, collected by Alloy. Any collector that reads journald or standard error works the same.
- A server deployment comes with its systemd unit, Alloy and Prometheus configuration, a dashboard and alerts.

**Non-Goals:**
- OTLP export of logs, metrics or traces from Legato. It can be added later beside the slog handler and the Prometheus registry.
- Tracing.
- Shipping agents' terminal output, conversations or task text anywhere.
- Host metrics (node_exporter does that).
- Replacing the credential audit log.

## Decisions

### D1. slog, one handler chain, events with stable names

All logging goes through `log/slog`. A message is a stable event name in dotted form (`agent.spawned`, `approval.decided`, `http.request`), never free text. A line's meaning is in its fields. A package logs through the logger it is given, or through `obs.Logger(ctx)` when the context carries one. The standard `log` package's output is redirected into slog at `info` with `event=log.legacy`, so a missed call still arrives structured, and a test fails when production code calls `log.Printf` (see D7).

The handler chain is `context fields → redaction → JSON or text handler`.

- **Context fields.** `obs.With(ctx, fields...)` adds fields that every line logged with that context carries: `request_id`, `client`, `task_id`, `execution_id`, `agent_kind`, `approval_id`, `workflow_run_id`. A request handler sets `request_id` and `client` once, and the service calls it makes log them without passing them along.
- **Redaction.** The redaction handler holds the values Legato knows are secret: the web auth token, broker tokens, credential values read during the process, placeholders' real values, and the 1Password service account token. It replaces them in every string field and the message. It also drops a field whose key names a secret (`token`, `secret`, `password`, `authorization`, `cookie`) unless the value is already `"[redacted]"`. It is a backstop: code does not log secrets in the first place.

*Alternative: zap or zerolog.* Rejected. slog is in the standard library, fast enough for this volume, and needs no new dependency.

### D2. Where each process logs

| Process | Default output |
| --- | --- |
| `legato serve` | JSON to standard error |
| TUI | JSON to `<state dir>/legato/legato.log` (XDG state, `~/.local/state/legato/` by default), rotated at 20 MB, keeping 3 files |
| Short-lived commands (hooks, CLI, `mcp serve`, bridges) | Text to standard error at `warn`, as today's errors |

- **Settings.** `observability.log.format` (`json`, `text`, `auto`: JSON when standard error is not a terminal) and `observability.log.level` (`debug`, `info`, `warn`, `error`; default `info`). `observability.log.file` sets the TUI's file; `-` sends it nowhere.
- **Why short-lived commands stay quiet.** Their important events happen in the host through the broker and are logged there.
- **Rotation** is a small writer in `internal/engine/obs`, not a dependency.

### D3. Request IDs and the client

- **Every API request and WebSocket connection** gets a `request_id`: 12 random hex characters, or a valid `X-Request-ID` header that the client sent. It goes back in the response header. The middleware puts it and the `client` in the request's context.
- **WebSocket messages** that cause work (`send_keys`, `agent_message`, verdicts) log the connection's `request_id` with a per-message sequence number.
- **Broker calls** log the caller's `task_id` and `execution_id`, taken from its token.
- **The client** is how the request was authenticated, and from where:
  - `auth:token` (the web auth token);
  - `device:<id>` when the request is signed by, or belongs to, a paired device that Legato can identify;
  - `broker:<execution>`;
  - `unauthenticated`.

  It is logged with the remote address and a shortened user agent. Behind a reverse proxy, the remote address is the proxy's unless `observability.trusted_proxies` lists it. This change adds that setting, which is empty by default.

### D4. What is never logged

- **Secret values of any kind.**
- **What people and agents write:** prompts, messages, terminal output, conversations, task titles and descriptions, plan and review text, file contents. These are logged as sizes (`prompt_bytes`) and IDs only.
  - Task titles are left out too. They often name customers or private work, and Loki keeps logs longer than a task lives. `task_id` links to the title in Legato.
- **Full URLs with query strings.** The WebSocket URL carries `?token=`. Requests are logged by route pattern (`/api/tasks/{id}/agent`) and path parameters by name.
- **Request and response bodies.**

### D5. Metrics: a separate listener, two kinds of metric

- **The endpoint.** `prometheus/client_golang`, served at `/metrics` on `observability.metrics.listen` (for example `0.0.0.0:9464`). It is off when empty, and it is separate from the web server. Scraping the web server would need Legato's full-access auth token.
- **Its token.** `observability.metrics.token_file` names a file (mode `0600`) holding a bearer token that Prometheus sends (`bearer_token_file` in its scrape configuration). The token is read again on each request, so it can rotate. A listener outside loopback without a token logs `metrics.unprotected` at `warn` at start. It is not refused: the person's Prometheus is on a trusted LAN.
- **Which processes export.** Only host processes (`legato serve`, the TUI): they own the broker, the agents and the services. One listener per host. A second host Legato on the same machine that finds the address taken logs `metrics.listen_failed` and goes on.
- **Two kinds of metric:**
  - **Events are counters and histograms,** counted where they happen: spawns, decisions, requests, uses. They reset when Legato restarts, which Prometheus's `rate()` and `increase()` handle.
  - **State is gauges read at scrape time** by a collector that queries the store: agents by state, pending approvals, open Attention items, runs by status. They are always right after a restart, and nothing has to keep them in step. Each scrape runs a few indexed SQLite queries, with a 2-second limit; a collector that fails sets `legato_collector_errors_total` and leaves out its metrics.
- **Label cardinality.** Labels take values from small, known sets: state, kind, outcome, profile, route pattern, credential name. Two exceptions:
  - `task_id` appears only on gauges about live agents (`legato_agent_running_seconds`, `legato_agent_info`). The series end with the agent, and there are a few dozen at most.
  - No title, path, prompt, repository branch or user agent is ever a label.

### D6. The metrics

All are prefixed `legato_`.

| Metric | Type | Labels | Source |
| --- | --- | --- | --- |
| `agents` | gauge | `state`, `agent_kind`, `sandboxed` | sessions, at scrape |
| `agent_info` | gauge (1) | `task_id`, `execution_id`, `agent_kind`, `state`, `sandboxed`, `role` | live sessions, at scrape |
| `agent_running_seconds` | gauge | `task_id`, `agent_kind` | live executions' start, at scrape |
| `agent_state_seconds_total` | counter | `state`, `agent_kind` | `state_intervals`, at scrape |
| `agent_spawns_total` | counter | `agent_kind`, `sandboxed`, `result` | on spawn |
| `agent_ends_total` | counter | `agent_kind`, `reason` (`exited`, `killed`, `died`, `completed`) | on end |
| `agent_session_seconds` | histogram | `agent_kind` | on end |
| `agent_messages_total` | counter | `source`, `result` | on queue and delivery |
| `native_bridges` | gauge | `agent_kind` | bound endpoints, at scrape |
| `approvals_pending` | gauge | `source` | at scrape |
| `approval_oldest_pending_seconds` | gauge | `source` | at scrape |
| `approval_decisions_total` | counter | `source`, `outcome`, `via` (`web`, `phone`, `signed`, `tui`, `expired`) | on decision |
| `approval_wait_seconds` | histogram | `source` | on decision |
| `credential_uses_total` | counter | `credential`, `operation`, `result` (`granted`, `refused`, `pending`) | on use |
| `credential_grants_active` | gauge | `credential` | at scrape |
| `proxy_requests_total` | counter | `profile`, `status_class` | on proxy response |
| `proxy_upstream_seconds` | histogram | `profile` | on proxy response |
| `ssh_logins_total` | counter | `result` | on sign |
| `workflow_runs` | gauge | `status` | at scrape |
| `workflow_run_seconds` | histogram | `workflow`, `result` | on run end |
| `workflow_step_failures_total` | counter | `step_type` | on failure |
| `swarm_subtasks` | gauge | `status` | at scrape |
| `attention_items` | gauge | `producer`, `urgency` | at scrape |
| `sync_seconds` | histogram | `provider`, `result` | on sync |
| `sync_errors_total` | counter | `provider`, `kind` (`auth`, `rate_limit`, `offline`, `other`) | on sync |
| `github_rate_limit_remaining` | gauge | none | last response seen |
| `http_requests_total` | counter | `route`, `method`, `status_class` | middleware |
| `http_request_seconds` | histogram | `route`, `method` | middleware |
| `websocket_clients` | gauge | none | hub |
| `broker_calls_total` | counter | `op`, `result` | broker handler |
| `build_info` | gauge (1) | `version`, `build_id`, `go_version` | start |
| `start_time_seconds` | gauge | none | start |
| `database_bytes` | gauge | none | at scrape |
| `database_busy_total` | counter | none | store |
| `collector_errors_total` | counter | `collector` | collector |

Go runtime and process metrics come from the standard collectors.

### D7. Converting the call sites

- **Each package's calls are replaced by events from the catalogue** (`log-events`), package by package.
- **An architecture test** fails on `log.Printf`, `log.Println` or `fmt.Fprintf(os.Stderr, …)` in non-test production code outside `cmd/legato` command output. `cmd/legato` writes command output on purpose.
- **A catalogue test** keeps every event name used in code listed in `docs/ai/observability.md`, and the list in step with the code. The names are constants in `internal/engine/obs/events.go`, so a typo does not compile.

### D8. Deployment assets

Under `deploy/`:
- `legato.service`: systemd unit for `legato serve`, `Restart=on-failure`, standard error to journald with `SyslogIdentifier=legato`.
- `alloy/legato.alloy`:
  - reads the journald unit;
  - parses the JSON;
  - promotes `level`, `component` and `event` to labels;
  - keeps `task_id`, `execution_id`, `request_id` and `client` as structured metadata;
  - drops `debug` unless enabled;
  - writes to a Loki endpoint given as an Alloy argument.
- `prometheus/legato.yml`: a scrape job with `bearer_token_file`.
- `grafana/legato.json`: a dashboard with these sections:
  - agents over time by state;
  - live agents and how long they have run;
  - spawns and ends;
  - approvals waiting and decision time;
  - credential uses and refusals;
  - workflows;
  - sync and the GitHub rate limit;
  - HTTP and WebSocket;
  - a Loki panel of `warn` and `error` events.
- `prometheus/legato-alerts.yml`, alert rules for:
  - Legato down;
  - an agent waiting on the person for over 30 minutes;
  - an approval pending for over 10 minutes;
  - credential refusals above a rate;
  - sync failing for 15 minutes;
  - the GitHub rate limit below 10%;
  - collector errors.

  Argus can take these in as a source later.

A test checks that the dashboard and alert rules refer only to metric names that exist in the registry.

## Risks / Trade-offs

- **[Log volume]** Activity changes are the most frequent event. Estimated tens of thousands of lines a day at 20 agents. If that is too much, `agent.activity_changed` moves to `debug` and start and end stay at `info`.
- **[Secrets in logs]**
  - Events are written with typed fields, never `%v` of a whole struct.
  - Route patterns are logged, not URLs.
  - The redaction handler is a backstop.
  - A test feeds every known secret kind through the handler.
- **[Scrape cost]** Collectors run SQLite queries each scrape. They are bounded by a 2-second limit and use indexed queries; the person's scrape interval is 15–60 s.
- **[A large mechanical change]** Converting 92 call sites touches many packages. It is done package by package, guarded by the architecture test, and it changes no behaviour except output.
- **[The TUI's log file]** A new file in the state folder. It is rotated, so it cannot grow without bound.

## Migration Plan

Logging changes output only. `legato serve` in a terminal gets text (`format: auto`); under systemd it gets JSON. Metrics are off until `observability.metrics.listen` is set. To roll back, run an earlier version. Nothing is stored.

## Open Questions

1. Should `agent.activity_changed` be `info` (the full picture in Loki) or `debug` (less volume)? This design starts at `info`.
2. `device:<id>` as the client: the web UI authenticates with the shared auth token, so only signed approvals and paired phones can be told apart today. Is a token per device worth a separate change?
3. An OTLP exporter for logs, metrics and traces, so a host without Alloy can ship directly: a later change.
