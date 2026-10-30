## ADDED Requirements

### Requirement: systemd unit
The repository SHALL ship `deploy/legato.service`, a systemd unit that runs `legato serve` as the person's user, restarts it on failure, and sends its standard error to journald with `SyslogIdentifier=legato`.

#### Scenario: Logs in journald
- **WHEN** Legato runs from the unit
- **THEN** `journalctl -u legato -o cat` SHALL show its JSON log lines

### Requirement: Alloy configuration
The repository SHALL ship `deploy/alloy/legato.alloy`. It SHALL:
- read the unit's journald entries;
- parse the JSON;
- set the labels `service="legato"`, `host`, `level`, `component` and `event`;
- keep `task_id`, `execution_id`, `request_id` and `client` as structured metadata;
- drop `debug` lines unless an argument enables them;
- write to a Loki endpoint given as an argument.

No field holding an ID SHALL become a label.

#### Scenario: Labels stay few
- **WHEN** a hundred agents have run in a day
- **THEN** the number of Loki streams from Legato SHALL depend only on level, component, event and host, not on the agents

### Requirement: Prometheus, dashboard and alerts
The repository SHALL ship:
- `deploy/prometheus/legato.yml`: a scrape job using `bearer_token_file`.
- `deploy/grafana/legato.json`: a dashboard with these panels:
  - agents over time by state;
  - live agents and how long they have run;
  - spawns and ends;
  - approvals waiting and time to decide;
  - credential uses and refusals;
  - workflows;
  - sync and the GitHub rate limit;
  - HTTP and WebSocket;
  - a Loki panel of `warn` and `error` events.
- `deploy/prometheus/legato-alerts.yml`, with alerts for:
  - Legato down;
  - an agent waiting on the person for over 30 minutes;
  - an approval pending for over 10 minutes;
  - credential refusals above a rate;
  - sync failing for 15 minutes;
  - the GitHub rate limit below 10%;
  - collector errors.

A test SHALL check that every metric name the dashboard and the rules use is one the endpoint exports.

#### Scenario: Renamed metric
- **WHEN** a metric is renamed in code and the dashboard still uses the old name
- **THEN** the test SHALL fail and name the metric and the file

### Requirement: Server guide
`docs/cloud-agents.md` SHALL explain:
- how to install the unit;
- how to point Alloy at Loki with the shipped configuration;
- how to set `observability.metrics.listen` and `token_file` and add the scrape job;
- how to import the dashboard and the rules.

#### Scenario: Following the guide
- **WHEN** the person follows the guide on a VM with Alloy and a Prometheus on the LAN
- **THEN** Legato's logs SHALL appear in Loki and its metrics in Prometheus without other changes
