## Why

Legato is moving from a desktop tool to a headless server on a VM (`legato serve`, `sandboxed-cloud-agents`). On a desktop, the person at the keyboard sees what happens. On a server nobody watches, the logs and metrics have to answer what happened, who did it, why it failed, and how the agents are doing. Today Legato has neither:
- 92 unstructured `log.Printf` calls with no levels and no fields;
- the TUI discards its log entirely;
- no metrics.

The person runs Grafana Alloy on their hosts (shipping to Loki) and Prometheus on the same LAN as the VM.

## What Changes

- **Structured logging.** Legato logs through `log/slog`. Every line is one event with a stable name (`agent.spawned`), a level, and fields: who caused it, and the task, execution, request or approval it concerns.
  - **Output.** JSON goes to standard error, which journald and Alloy pick up. The TUI writes to a log file instead of discarding its log.
  - **Redaction.** A redacting handler is the last line of defence against a secret reaching a log.
- **Logging what a remote service must.** Every state change, every decision, every call to another system, and every request from outside, listed as a catalogue of events by area:
  - start and stop;
  - API requests and WebSocket connections;
  - sign-in and devices;
  - agents and messages to agents;
  - approvals and credentials;
  - workflows and swarms;
  - sync;
  - the broker;
  - background work.

  Requests get an ID that follows the work they cause. What people and agents write (prompts, messages, terminal output, task text) is never logged; only its size is.
- **Metrics.** A Prometheus endpoint (`/metrics`, the text exposition format) on its own listener, off unless configured, with an optional bearer token. It covers:
  - agents by state, kind and sandbox, time in each state, spawns and ends, session lengths, and how long each live agent has run;
  - approvals, credentials and the proxy;
  - workflows and swarms;
  - Attention;
  - sync and the GitHub rate limit;
  - HTTP, WebSocket and broker traffic;
  - the process.
- **Deployment assets** in the repository:
  - a systemd unit for `legato serve`;
  - an Alloy configuration that ships its journald output to Loki with low-cardinality labels;
  - a Prometheus scrape configuration;
  - a Grafana dashboard;
  - alert rules.
- **Configuration:** a new `observability:` block (log level, format and file; metrics listener and token).

Not in this change: pushing logs, metrics or traces over OTLP from Legato itself, and traces. These are left for a later change; the logging and metrics layers are built so an OTLP exporter can be added beside them.

## Capabilities

### New Capabilities
- `structured-logging`: The logger: slog, JSON and text output, levels, the log file, standard fields, context propagation, request IDs, and redaction.
- `log-events`: The catalogue of events Legato logs, by area, with their names, levels and fields, and what is never logged.
- `metrics-endpoint`: The Prometheus endpoint, its listener and token, and the metrics with their types and labels.
- `observability-assets`: The systemd unit, Alloy, Prometheus, Grafana and alert-rule files shipped for a server deployment.

### Modified Capabilities

None. Existing requirements do not change; this change adds logging and metrics alongside them.

## Impact

- **Code:**
  - `internal/engine/obs` (new): the slog handler, redaction, request ID context, metrics registry helpers.
  - Every package that logs today: the `log.Printf` calls are replaced.
  - `internal/server`: request logging middleware and request IDs.
  - `internal/service`: events at state changes and decisions.
  - `cmd/legato`: logger setup per runtime, metrics listener.
  - `config/`: the `observability` block.
- **Dependencies:** `github.com/prometheus/client_golang` (pure Go).
- **Config:** `observability.log.{level, format, file}` and `observability.metrics.{listen, token_file}`.
- **Docs:** `docs/ai/observability.md` (new), `docs/ai/config.md`, `docs/cloud-agents.md` (the VM guide gains logs and metrics).
- **Unchanged:** the credential audit log stays the record of credential use, in the database. Logs point to it; they do not replace it.
