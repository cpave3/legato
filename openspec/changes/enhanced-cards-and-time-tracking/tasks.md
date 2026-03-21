## 1. Database Migration & Store Layer

- [ ] 1.1 Create migration `007_state_intervals.sql` with `state_intervals` table (id, task_id FK, state CHECK, started_at, ended_at, index on task_id)
- [ ] 1.2 Add `StateInterval` struct to store package and implement `RecordStateTransition(ctx, taskID, state)` — closes open interval, opens new one if state is non-empty, idempotent for same-state
- [ ] 1.3 Implement `GetStateDurations(ctx, taskID)` — returns map[string]time.Duration aggregating all intervals (including open ones using current time)
- [ ] 1.4 Implement `GetStateDurationsBatch(ctx, taskIDs)` — single-query batch version returning map[taskID]map[state]duration
- [ ] 1.5 Write tests for all store methods: transition recording (open/close/idempotent/clear), duration aggregation (completed intervals, open intervals, no intervals, batch)

## 2. Service Layer Integration

- [ ] 2.1 Extend `AgentService` (or its store dependency) to call `RecordStateTransition` when `UpdateAgentActivity` is invoked
- [ ] 2.2 Extend `ReconcileSessions` to close orphaned intervals for dead agent sessions
- [ ] 2.3 Add `GetTaskDurations(ctx, taskIDs)` method to service layer that wraps the store batch query
- [ ] 2.4 Write tests: activity update triggers interval recording, reconcile closes orphaned intervals, duration query passthrough

## 3. Card Rendering Enhancement

- [ ] 3.1 Add `WorkingDuration` and `WaitingDuration` fields (time.Duration) to `board.CardData`
- [ ] 3.2 Implement `formatDuration(d time.Duration) string` helper — returns "", "<1m", "Xm", or "Xh Ym"
- [ ] 3.3 Update `RenderCard` to add agent status line (line 3) when card has agent data or duration history — show state icon, label, and cumulative duration
- [ ] 3.4 Update `RenderCard` for inactive-agent-with-history case — show total working/waiting durations
- [ ] 3.5 Implement uniform card height within columns — pad shorter cards to match tallest card in column
- [ ] 3.6 Write tests for duration formatting, card rendering with/without agent data, uniform height padding

## 4. Data Flow Wiring

- [ ] 4.1 Update `app.go` DataLoadedMsg handler to call `GetTaskDurations` batch query and populate `CardData.WorkingDuration`/`WaitingDuration` via a new `board.SetDurations(map[string]DurationData)` method
- [ ] 4.2 Write test verifying duration data flows from service through app to board CardData
