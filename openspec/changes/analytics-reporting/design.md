## Context

Legato tracks granular session data in SQLite: `state_intervals` records working/waiting time per task, `agent_sessions` tracks tmux session lifecycles, and `tasks` has status, timestamps, workspace assignment, and PR metadata. Currently this data is only surfaced per-card (duration labels on the board). There's no way to see aggregate trends or export summaries.

The user wants daily/weekly/monthly reports with a TUI view now and potential web rendering later, so the report computation must be fully decoupled from presentation.

## Goals / Non-Goals

**Goals:**
- Query and aggregate existing data into meaningful reports (no new data collection)
- Clean separation: engine (SQL queries) → service (report models) → presentation (TUI or markdown)
- TUI report view with period navigation and key metrics
- Markdown export for sharing
- Extensible for future presenters (web dashboard, JSON API)

**Non-Goals:**
- Real-time streaming analytics or dashboards
- Historical snapshots stored in DB (reports are computed on-demand from raw data)
- Modifying existing data collection — we work with what `state_intervals`, `agent_sessions`, and `tasks` already capture
- Web server for reports (future work, but the backend should not preclude it)
- Comparative analytics across projects or multi-user reporting

## Decisions

### 1. Report as a computed view, not stored data

Reports are computed on-demand from existing tables. No new tables or materialized views.

**Rationale**: The raw data already exists and is small enough for SQLite to aggregate quickly (seconds of delay at most for months of data). Storing computed reports adds complexity (staleness, migrations) for no benefit at this scale.

**Alternative considered**: Pre-computed daily rollups in a `report_snapshots` table. Rejected — premature optimization, adds migration burden, and raw queries on indexed columns will be fast enough.

### 2. Three-layer report architecture following existing conventions

```
engine/analytics/   → SQL queries, returns raw aggregation structs
service/            → ReportService: time range logic, grouping, report model assembly
tui/report/         → TUI rendering (charts, tables, navigation)
```

Plus a `service/export.go` for markdown generation (service layer, no TUI dependency).

**Rationale**: Matches the existing engine→service→tui layering. The service layer produces `Report` structs that any presenter can consume. Markdown export lives in service because it's a serialization format, not a UI concern.

### 3. Report model as plain Go structs

```go
type Report struct {
    Period    TimeRange
    Summary   Summary        // top-level totals
    ByDay     []DayBreakdown // daily granularity within period
    ByTask    []TaskStats    // per-task breakdown
    ByWorkspace []WorkspaceStats
}

type Summary struct {
    TotalWorkingDuration  time.Duration
    TotalWaitingDuration  time.Duration
    TasksCompleted        int  // moved to Done in period
    TasksCreated          int
    AgentSessionCount     int
    PRsMerged             int
}
```

**Rationale**: Plain structs with no rendering logic. Any presenter (TUI, markdown, JSON, web template) can consume these directly. The `ByDay` slice enables sparkline/chart rendering without the presenter needing SQL access.

### 4. Time range as first-class concept

```go
type TimeRange struct {
    Start time.Time
    End   time.Time
    Label string // "Today", "This Week", "March 2026", etc.
}
```

Service provides helpers: `Today()`, `ThisWeek()`, `ThisMonth()`, `Last7Days()`, `Last30Days()`, plus `Previous()`/`Next()` for navigation.

**Rationale**: Period navigation is the primary interaction in the TUI. Making it a named type with helpers keeps the navigation logic in the service layer, not scattered across the UI.

### 5. TUI report as a new view (not an overlay)

New `viewReport` enum value alongside `viewBoard`/`viewDetail`/`viewAgents`. Full-screen view with its own keybindings and layout.

**Rationale**: Reports need significant screen real estate for charts and tables. An overlay would be too cramped. The existing view-switching pattern (`A` for agents) works well — `S` for stats/reports.

**Alternative considered**: Overlay panel. Rejected — not enough space for meaningful charts, and overlays are designed for quick actions, not browsing.

### 6. Lipgloss-based charts (no external charting library)

Bar charts and summary tables rendered with lipgloss styling. Horizontal bars for duration breakdowns, simple ASCII sparklines for daily trends.

**Rationale**: Keeps dependencies minimal. The charts needed are simple (bar charts, tables, sparklines). Lipgloss already provides the styling primitives. A charting library would be overkill and may not integrate well with bubbletea's rendering model.

### 7. Markdown export via clipboard or file

`R` (shift-r) from report view copies markdown to clipboard. Same clipboard abstraction already used for PR URLs.

**Rationale**: Clipboard is the fastest path to sharing (paste into Slack, PR description, etc.). File export can be added later but clipboard covers the primary use case.

## Risks / Trade-offs

**[Performance on large datasets]** → SQLite aggregation queries on `state_intervals` could slow down with months of data. Mitigation: queries are bounded by time range (WHERE clause on timestamps), and indexes on `started_at` keep scans tight. Monitor and add indexes if needed.

**[Incomplete data for historical periods]** → `state_intervals` only exists since the feature was added. Early tasks won't have duration data. Mitigation: Report shows "no data" gracefully for tasks without intervals. Don't fake data.

**[TUI chart fidelity]** → Terminal charts are inherently low-resolution. Mitigation: Focus on relative comparisons (bar proportions) rather than precise values. Show exact numbers alongside visual bars. Markdown export provides a cleaner format for detailed review.

**[Time zone handling]** → SQLite `datetime('now')` stores UTC. Report periods (today, this week) depend on local time. Mitigation: Use `time.Now().Local()` for range boundaries, convert to UTC for SQL queries. Document that reports use local time.

## Open Questions

- Should the report view support filtering by workspace (like the board does), or always show all workspaces with a breakdown? Leaning toward: always show all with per-workspace breakdown, since the point is aggregate visibility.
- What keybinding for the report view? `S` (stats) seems natural and is currently unbound from the board.
