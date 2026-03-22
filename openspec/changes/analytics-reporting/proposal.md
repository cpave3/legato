## Why

Legato already tracks rich session data — working/waiting durations per task, agent session lifetimes, task status transitions, and PR metadata — but provides no way to review this data in aggregate. Developers using AI agents need visibility into how their time (and their agents' time) is being spent across days and weeks to identify bottlenecks, justify tooling investments, and improve workflows.

## What Changes

- New analytics engine that queries and aggregates existing `state_intervals`, `agent_sessions`, and `tasks` data into daily/weekly/monthly reports
- New report data model decoupled from presentation — computable stats returned as structured Go types, renderable by any frontend
- TUI report view accessible from the board (new keybinding) showing charts/tables for key metrics
- Markdown export of reports for sharing in PRs, Slack, standups, etc.
- Foundation for future web dashboard (report backend is presentation-agnostic)

## Capabilities

### New Capabilities
- `analytics-engine`: Core analytics computation — queries SQLite for duration aggregations, session counts, task throughput, and PR metrics across configurable time ranges. Returns structured report data (no rendering). Lives in `internal/engine/analytics/`.
- `report-service`: Business logic layer that orchestrates analytics queries, applies time range selection (day/week/month/custom), groups by workspace/task/agent, and produces report models. Lives in `internal/service/`. Consumes analytics engine, produces presentation-agnostic report structs.
- `report-tui`: TUI presentation of reports — sparkline-style charts, summary tables, period navigation. New view accessible via keybinding from the board. Lives in `internal/tui/report/`.
- `report-export`: Markdown report generation from report models. Produces formatted markdown suitable for clipboard/file export. Lives alongside report service, no TUI dependency.

### Modified Capabilities

_(none — this is additive, no existing behavior changes)_

## Impact

- **New packages**: `internal/engine/analytics/`, `internal/tui/report/`
- **Modified packages**: `internal/service/` (new ReportService), `internal/tui/` (new view routing for report view), `internal/tui/board/` (keybinding to open reports)
- **Database**: Read-only queries against existing tables (`state_intervals`, `agent_sessions`, `tasks`, `workspaces`). No new migrations needed.
- **Dependencies**: Possibly a lightweight TUI charting helper (or custom lipgloss rendering). No new external deps required — lipgloss can render bar charts.
- **Config**: Optional `reports` config section for defaults (time range, grouping preferences)
