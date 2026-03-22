## ADDED Requirements

### Requirement: Report view accessible from board
The TUI SHALL provide a keybinding (`S`) from the board view that opens a full-screen report view showing analytics for the current period.

#### Scenario: Open report view
- **WHEN** user presses `S` on the board
- **THEN** the TUI switches to the report view showing today's summary

#### Scenario: Return to board
- **WHEN** user presses `esc` or `S` in the report view
- **THEN** the TUI returns to the board view

### Requirement: Period selection and navigation
The report view SHALL display the current time period and allow switching between period types and navigating to adjacent periods.

#### Scenario: Cycle period type
- **WHEN** user presses `tab` in report view
- **THEN** the period cycles through: Day → Week → Month → Day

#### Scenario: Navigate to previous period
- **WHEN** user presses `h` or left arrow in report view
- **THEN** the view shows the previous period (e.g., yesterday, last week)

#### Scenario: Navigate to next period
- **WHEN** user presses `l` or right arrow in report view
- **THEN** the view shows the next period (up to current, no future)

### Requirement: Summary panel
The report view SHALL display a summary panel with key metrics: total working time, total waiting time, working/waiting ratio, tasks completed, tasks created, agent sessions, and PRs merged.

#### Scenario: Display summary with data
- **WHEN** the current period has working=4h, waiting=1h30m, 3 tasks completed, 5 sessions
- **THEN** the summary panel shows these values formatted with duration strings and counts

#### Scenario: Display summary with no data
- **WHEN** the current period has no activity
- **THEN** the summary panel shows zeros/dashes, not errors

### Requirement: Daily activity chart
The report view SHALL display a horizontal bar chart showing working and waiting time per day within the selected period.

#### Scenario: Weekly chart shows 7 bars
- **WHEN** viewing a weekly report
- **THEN** the chart shows 7 rows (Mon-Sun), each with a working bar and waiting bar, scaled proportionally to the max day

#### Scenario: Day with no activity shows empty bar
- **WHEN** a day in the period has zero activity
- **THEN** that day's row shows the label but no bars

### Requirement: Task breakdown table
The report view SHALL display a table of tasks sorted by total working duration (descending), showing task ID, title (truncated), working time, and waiting time.

#### Scenario: Task table with multiple tasks
- **WHEN** the period has 5 tasks with activity
- **THEN** the table shows all 5 rows sorted by working duration, highest first

#### Scenario: Scrollable task list
- **WHEN** the task list exceeds the visible area
- **THEN** user can scroll with `j`/`k` within the task table section

### Requirement: Workspace breakdown
The report view SHALL show per-workspace totals when workspaces are configured, with colored workspace names matching board colors.

#### Scenario: Multiple workspaces with activity
- **WHEN** the period has activity in "frontend" (blue) and "backend" (green) workspaces
- **THEN** the workspace section shows each with its color and duration totals

### Requirement: Copy report to clipboard
The report view SHALL support copying the current report as formatted markdown to the system clipboard.

#### Scenario: Copy report
- **WHEN** user presses `R` (shift-r) in report view
- **THEN** the markdown-formatted report is copied to the clipboard and the status bar confirms "Report copied"
