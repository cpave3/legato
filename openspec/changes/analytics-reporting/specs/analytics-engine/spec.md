## ADDED Requirements

### Requirement: Duration aggregation by time range
The analytics engine SHALL query `state_intervals` and return total working and waiting durations for all tasks within a given time range. Intervals that span the range boundary SHALL be clipped to the range (only the portion within the range counts). Open intervals (ended_at IS NULL) SHALL use the current time as the end.

#### Scenario: Aggregate durations for a single day
- **WHEN** querying durations for 2026-03-20 00:00 to 2026-03-21 00:00
- **THEN** the engine returns total working and waiting durations considering only interval portions within that day

#### Scenario: Open interval spans into the query range
- **WHEN** a working interval started at 2026-03-19 23:00 with no ended_at
- **AND** querying for 2026-03-20
- **THEN** the interval contributes time from 2026-03-20 00:00 to now (clipped at range start)

#### Scenario: No intervals in range
- **WHEN** querying a time range with no state_intervals
- **THEN** the engine returns zero durations (not an error)

### Requirement: Daily breakdown within range
The analytics engine SHALL return per-day duration totals within a given time range, enabling chart rendering at daily granularity.

#### Scenario: Weekly range returns 7 day buckets
- **WHEN** querying a 7-day range
- **THEN** the engine returns exactly 7 daily entries (one per day), each with working and waiting totals

#### Scenario: Days with no activity return zero durations
- **WHEN** a day within the range has no state_intervals
- **THEN** that day's entry has zero working and zero waiting duration

### Requirement: Per-task duration breakdown
The analytics engine SHALL return working and waiting durations grouped by task_id for a given time range.

#### Scenario: Multiple tasks with overlapping intervals
- **WHEN** querying a range where tasks A and B both have intervals
- **THEN** the engine returns separate duration totals for each task

#### Scenario: Task with no intervals in range
- **WHEN** a task has intervals outside the queried range but none inside
- **THEN** that task does not appear in the results

### Requirement: Task throughput counts
The analytics engine SHALL count tasks created and tasks completed (moved to Done status) within a given time range.

#### Scenario: Count tasks created in range
- **WHEN** querying a week where 3 tasks were created
- **THEN** tasks_created returns 3

#### Scenario: Count tasks completed in range
- **WHEN** querying a week where 2 tasks were archived (moved to Done)
- **THEN** tasks_completed returns 2

### Requirement: Agent session counts
The analytics engine SHALL count agent sessions started within a given time range.

#### Scenario: Count sessions in range
- **WHEN** querying a day where 5 agent sessions were started
- **THEN** session_count returns 5

### Requirement: Per-workspace breakdown
The analytics engine SHALL return duration and throughput metrics grouped by workspace for a given time range.

#### Scenario: Tasks across multiple workspaces
- **WHEN** querying a range with tasks in workspace "frontend" and "backend"
- **THEN** the engine returns separate metrics for each workspace

#### Scenario: Tasks with no workspace
- **WHEN** querying a range with tasks that have no workspace_id
- **THEN** those tasks appear under a nil/unassigned workspace group
