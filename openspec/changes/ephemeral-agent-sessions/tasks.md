## 1. Database Migration

- [x] 1.1 Create migration `011_ephemeral.sql` adding `ephemeral INTEGER NOT NULL DEFAULT 0` to `tasks` table
- [x] 1.2 Update store listing queries (`ListTasksByStatus`, `ListTasksByStatusAndWorkspace`, `SearchCards`) to filter `AND ephemeral = 0`

## 2. Store Layer — Ephemeral Task Creation

- [x] 2.1 Add `CreateEphemeralTask(ctx, title) (string, error)` to store — generates 8-char ID, inserts task with `ephemeral = 1`, first column status, returns task ID
- [x] 2.2 Write tests for ephemeral task creation and board query exclusion

## 3. Service Layer — SpawnEphemeralAgent

- [x] 3.1 Add `SpawnEphemeralAgent(ctx, title, width, height) error` to `AgentService` — creates ephemeral task via store, then delegates to existing spawn flow
- [x] 3.2 Update `AgentService` interface to include `SpawnEphemeralAgent`
- [x] 3.3 Write tests for ephemeral agent spawn (task created, tmux session started, DB record inserted)

## 4. TUI — Ephemeral Spawn Overlay

- [x] 4.1 Create `EphemeralSpawnOverlay` in `internal/tui/overlay/` — single text input for title, enter submits, esc cancels, empty defaults to "Ephemeral session"
- [x] 4.2 Wire `s` key in agent view to open the ephemeral spawn overlay
- [x] 4.3 Handle overlay submit in `app.go` — call `AgentService.SpawnEphemeralAgent`, refresh agent list
- [x] 4.4 Update agent view empty state message to reference `s` for ephemeral sessions

## 5. Integration & Polish

- [x] 5.1 Verify ephemeral agents show title in agent sidebar and terminal header
- [x] 5.2 Verify state tracking (working/waiting durations) works for ephemeral agents
- [x] 5.3 Verify ephemeral tasks do not appear on board, in search, or in card counts
