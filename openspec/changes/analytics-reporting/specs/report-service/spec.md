## ADDED Requirements

### Requirement: Generate report for named time period
The report service SHALL accept a time period (today, this week, this month, last 7 days, last 30 days) and return a complete Report struct containing summary, daily breakdown, per-task stats, and per-workspace stats.

#### Scenario: Generate today's report
- **WHEN** requesting a report for "today"
- **THEN** the service returns a Report with TimeRange spanning midnight-to-now in local time, with all metrics populated

#### Scenario: Generate this week's report
- **WHEN** requesting a report for "this week"
- **THEN** the service returns a Report with TimeRange from Monday 00:00 to now (local time), daily breakdown for each day so far

#### Scenario: Generate this month's report
- **WHEN** requesting a report for "this month"
- **THEN** the service returns a Report with TimeRange from the 1st of the current month to now

### Requirement: Period navigation
The report service SHALL provide Previous and Next navigation for any time period, returning the adjacent period of the same type.

#### Scenario: Navigate to previous week
- **WHEN** viewing "This Week" and requesting Previous
- **THEN** the service returns a Report for the prior Monday-to-Sunday

#### Scenario: Navigate to next day from a past day
- **WHEN** viewing 2026-03-18 and requesting Next
- **THEN** the service returns a Report for 2026-03-19

#### Scenario: Cannot navigate past current period
- **WHEN** viewing the current period and requesting Next
- **THEN** the service returns nil or the same period (no future data)

### Requirement: Report model is presentation-agnostic
The Report struct SHALL contain only data types (durations, counts, strings, time values) with no rendering logic, TUI imports, or format-specific fields.

#### Scenario: Report struct has no tui imports
- **WHEN** inspecting the report service package
- **THEN** it has no imports from internal/tui or any lipgloss/bubbletea packages

### Requirement: Task stats include metadata
Per-task stats in the Report SHALL include task title, task ID, workspace name, provider type, and working/waiting durations — sufficient for any presenter to render a meaningful task breakdown.

#### Scenario: Task stats for a local task
- **WHEN** generating a report containing a local task with 2h working, 30m waiting
- **THEN** the TaskStats entry includes id, title, workspace name (or empty), provider="" , working=2h, waiting=30m

#### Scenario: Task stats for a Jira task
- **WHEN** generating a report containing a Jira-synced task
- **THEN** the TaskStats entry includes the remote_id as display ID and provider="jira"

### Requirement: Summary includes PR metrics
The Report summary SHALL include a count of PRs merged within the time range, derived from pr_meta data on tasks.

#### Scenario: Count merged PRs
- **WHEN** 2 tasks had their pr_meta state change to MERGED within the range
- **THEN** summary.PRsMerged equals 2

#### Scenario: No PR data
- **WHEN** no tasks have pr_meta
- **THEN** summary.PRsMerged equals 0
