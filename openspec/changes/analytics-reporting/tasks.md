## 1. Analytics Engine (internal/engine/analytics/)

- [x] 1.1 Create `internal/engine/analytics/` package with `TimeRange` struct and period helpers (`Today`, `ThisWeek`, `ThisMonth`, `Last7Days`, `Last30Days`, `Previous`, `Next`) — all using local time, converting to UTC for SQL. Tests for boundary correctness (week start=Monday, month start, no-future guard).
- [x] 1.2 Implement `QueryDurations(ctx, db, timeRange)` — aggregates total working/waiting from `state_intervals` with range clipping (intervals spanning boundaries contribute only the in-range portion). Open intervals use `datetime('now')`. Returns `DurationResult{Working, Waiting time.Duration}`. Tests with real SQLite: full-in-range, partial overlap, open intervals, empty range.
- [x] 1.3 Implement `QueryDailyBreakdown(ctx, db, timeRange)` — returns `[]DayEntry{Date, Working, Waiting}` with one entry per calendar day in range. Days with no data return zero durations. Tests: 7-day range produces 7 entries, sparse data fills gaps with zeros.
- [x] 1.4 Implement `QueryTaskBreakdown(ctx, db, timeRange)` — returns `[]TaskDuration{TaskID, Working, Waiting}` grouped by task_id, filtered to range. Tests: multiple tasks, task outside range excluded.
- [x] 1.5 Implement `QueryThroughput(ctx, db, timeRange)` — returns `Throughput{TasksCreated, TasksCompleted, AgentSessions, PRsMerged}`. Created = tasks.created_at in range. Completed = tasks.archived_at in range. Sessions = agent_sessions.started_at in range. PRsMerged = tasks where pr_meta JSON state="MERGED" and pr_meta updated_at in range. Tests for each counter.
- [x] 1.6 Implement `QueryWorkspaceBreakdown(ctx, db, timeRange)` — returns `[]WorkspaceStats{WorkspaceID, WorkspaceName, Working, Waiting, TaskCount}`. Joins tasks↔state_intervals↔workspaces. Null workspace_id grouped as "Unassigned". Tests: multiple workspaces, unassigned tasks.

## 2. Report Service (internal/service/)

- [x] 2.1 Define `Report`, `Summary`, `DayBreakdown`, `TaskStats`, `WorkspaceStats` structs in `internal/service/report_model.go`. Verify no TUI imports. All fields are plain types (time.Duration, int, string, time.Time).
- [x] 2.2 Define `ReportService` interface and implementation in `internal/service/report.go`. Constructor takes `*sqlx.DB` (same pattern as other services). `GenerateReport(ctx, period)` calls analytics engine queries, enriches TaskStats with title/workspace from tasks table, assembles Report. Tests with real SQLite + seed data.
- [x] 2.3 Implement period navigation: `NextPeriod(current TimeRange)` and `PrevPeriod(current TimeRange)` on the service. `Next` returns nil/same if at current period. Tests for day/week/month navigation in both directions.

## 3. Markdown Export (internal/service/)

- [x] 3.1 Implement `ExportMarkdown(report Report) string` in `internal/service/report_export.go`. Produces formatted markdown with: header (period label), summary table (metric|value), daily breakdown table (day|working|waiting), task table (ID|title|working|waiting sorted by working desc). Empty report shows "No activity recorded". Tests: full report, empty report, edge cases (zero durations).

## 4. TUI Report View (internal/tui/report/)

- [x] 4.1 Create `internal/tui/report/` package with `Model` struct implementing bubbletea update/view pattern (concrete type like board/agents). State: current Report, current period type (day/week/month), loading bool, selected section for scroll. `Init` triggers data load command.
- [x] 4.2 Implement summary panel rendering — lipgloss-styled box with key metrics (working, waiting, ratio, completed, created, sessions, PRs merged). Format durations with existing `formatDuration` pattern.
- [x] 4.3 Implement daily activity chart — horizontal bar chart with lipgloss. Each day = one row with label (Mon, Tue...), working bar (green), waiting bar (blue), scaled to terminal width. Max value determines scale.
- [x] 4.4 Implement task breakdown table — styled table with ID, title (truncated to fit), working, waiting columns. Scrollable with j/k when focused. Highlight selected row.
- [x] 4.5 Implement workspace breakdown section — one row per workspace with colored name dot, working/waiting totals. Include "Unassigned" if present.
- [x] 4.6 Implement keybindings: `tab` cycles period type, `h`/`l` navigates periods, `j`/`k` scrolls task table, `R` copies markdown, `esc`/`S` returns to board.

## 5. Wire Into App

- [x] 5.1 Add `viewReport` to view enum in `internal/tui/app.go`. Add `report.Model` field to App. Handle `S` keybinding from board to switch to report view. Handle `esc`/`S` from report to return to board. Forward messages to report model when active.
- [x] 5.2 Wire `ReportService` creation in `cmd/legato/main.go` — construct with `store.DB()`, pass to `NewApp`. Report service is always available (no optional provider dependency).
- [x] 5.3 Connect clipboard export — `R` in report view calls `ExportMarkdown`, pipes to `clipboard.Copy`. Show status bar confirmation.
- [x] 5.4 Add `S` to help overlay keybinding list.

## 6. Polish

- [x] 6.1 Handle loading state — show spinner while report generates (same pattern as board data loading). Report generation is a tea.Cmd returning ReportLoadedMsg.
- [x] 6.2 Handle window resize — report view responds to WindowSizeMsg, adjusts chart widths and table column sizes.
- [x] 6.3 Test TUI model state via Update() — verify period cycling, navigation bounds, view switching messages. Same test pattern as board/agents (no ANSI output testing).
