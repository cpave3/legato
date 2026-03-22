## ADDED Requirements

### Requirement: Markdown report generation
The report export SHALL convert a Report struct into a formatted markdown string suitable for sharing in PRs, Slack, or documentation.

#### Scenario: Full report with all sections
- **WHEN** exporting a Report with summary, daily breakdown, and task stats
- **THEN** the markdown output includes a header with period label, summary table, daily chart (text-based), and task breakdown table

#### Scenario: Report with no activity
- **WHEN** exporting a Report with zero durations and counts
- **THEN** the markdown output shows the period header and "No activity recorded for this period"

### Requirement: Markdown summary section
The exported markdown SHALL include a summary section with working time, waiting time, tasks completed, tasks created, agent sessions, and PRs merged formatted as a markdown table.

#### Scenario: Summary table format
- **WHEN** exporting a report with working=6h30m, waiting=2h, completed=4, created=7, sessions=12, PRs=2
- **THEN** the markdown contains a table with metric/value columns showing formatted durations and counts

### Requirement: Markdown daily breakdown
The exported markdown SHALL include a daily breakdown showing each day's working and waiting durations as a text-based bar chart or table.

#### Scenario: Weekly breakdown as table
- **WHEN** exporting a weekly report
- **THEN** the markdown includes a table with day, working, waiting columns for each day in the period

### Requirement: Markdown task breakdown
The exported markdown SHALL include a task breakdown table sorted by working duration, showing task ID, title, working time, and waiting time.

#### Scenario: Task table in markdown
- **WHEN** exporting a report with 3 active tasks
- **THEN** the markdown includes a table with ID, Title, Working, Waiting columns sorted by working desc

### Requirement: Export has no TUI dependencies
The markdown export function SHALL live in the service layer and depend only on the Report struct and standard library. It SHALL NOT import any TUI, lipgloss, or bubbletea packages.

#### Scenario: Package imports are clean
- **WHEN** inspecting the export package imports
- **THEN** there are no imports from internal/tui or charting/styling libraries
