Every task is test first (the tdd skill). Keep `docs/ai/observability.md` (new) current as each part lands: the event catalogue, the metrics and the setup.

## 1. Logging core

- [ ] 1.1 `observability` config block (`log.level`, `log.format`, `log.file`, `trusted_proxies`, `metrics.listen`, `metrics.token_file`) with validation (`config/`, `docs/ai/config.md`)
- [ ] 1.2 `internal/engine/obs`: the handler chain (context fields, redaction, JSON/text), event name constants, `obs.With`/`obs.Logger(ctx)`, the rotating file writer
- [ ] 1.3 Redaction: known secret values registered by the processes that hold them (auth token, broker tokens, credential values read, placeholders, the 1Password token); keys that name secrets; a test feeding every kind through
- [ ] 1.4 Logger setup per runtime: `serve` to standard error (`auto` format), TUI to the rotating file (replacing `io.Discard`), short-lived commands at `warn`; standard `log` output carried in as `log.legacy`
- [ ] 1.5 Architecture test: no `log.Printf`/`log.Println`/direct standard-error writes in production code outside command output; catalogue test: every event constant listed in `docs/ai/observability.md`

## 2. Requests and identity

- [ ] 2.1 HTTP middleware: request ID (generated or a well-formed `X-Request-ID`), `client`, remote address with `trusted_proxies`, route pattern; `http.request` with status and duration; levels by status
- [ ] 2.2 WebSocket: `ws.connected`/`ws.disconnected`, request ID on work-causing messages; `auth.refused`
- [ ] 2.3 Broker: `broker.call`, `broker.refused`, caller fields from the token
- [ ] 2.4 Context propagation: request fields reach the service events they cause (a spawn from the web carries the request's `request_id`)

## 3. Events by area

Each converts the area's `log.Printf` calls and adds the catalogue's events, with tests for the fields and for what must not appear.

- [ ] 3.1 Start and stop: `legato.started`, `legato.stopping`, `legato.stopped`, `config.loaded` (debug)
- [ ] 3.2 Pairing and devices; signed approval verification
- [ ] 3.3 Agents: spawn, spawn failure, restart, resume, kill, end, activity, bridges, messages, `sandbox.refused`, `agent.launch` (debug)
- [ ] 3.4 Approvals and credentials, the proxy, SSH logins, rule matches (debug)
- [ ] 3.5 Workflows, swarms, Attention
- [ ] 3.6 Sync (Jira, GitHub), `github.rate_limit_low`, channel and broker hosting, reconciles
- [ ] 3.7 Remaining `log.Printf` calls converted; the architecture test passes with no exceptions

## 4. Metrics

- [ ] 4.1 `prometheus/client_golang`; the `/metrics` listener (off by default), bearer token from `token_file` read per request, `metrics.unprotected` and `metrics.listen_failed`
- [ ] 4.2 Collectors read at scrape time with a 2-second limit and `collector_errors_total`: agents, agent info and running time, state seconds, bridges, approvals, grants, workflow runs, swarm sub-tasks, Attention, database size
- [ ] 4.3 Counters and histograms where events happen: spawns, ends, session length, messages, approval decisions and waits, credential uses, proxy requests and upstream time, SSH logins, workflow run time and step failures, sync time and errors, GitHub rate limit, HTTP requests, WebSocket clients, broker calls, database busy
- [ ] 4.4 Build info, start time, Go runtime and process collectors
- [ ] 4.5 Tests: label sets bounded (no title, path or prompt), live-agent series removed on end, gauges right after a restart, route patterns as labels

## 5. Deployment assets and docs

- [ ] 5.1 `deploy/legato.service`
- [ ] 5.2 `deploy/alloy/legato.alloy` (journald, JSON parsing, labels, structured metadata, debug drop)
- [ ] 5.3 `deploy/prometheus/legato.yml` and `legato-alerts.yml`
- [ ] 5.4 `deploy/grafana/legato.json`
- [ ] 5.5 Test: every metric name in the dashboard and the rules exists in the registry
- [ ] 5.6 `docs/cloud-agents.md`: install the unit, Alloy to Loki, metrics listener and token, scrape job, dashboard and rules; `docs/ai/observability.md` complete
- [ ] 5.7 Check on a scratch Legato: JSON lines with the catalogue's fields, no secret or written text in a full day's sample run, `/metrics` scraped by a local Prometheus, the dashboard loads in a scratch Grafana

## 6. Finish

- [ ] 6.1 Resolve or carry forward the design's open questions
- [ ] 6.2 Archive the change
