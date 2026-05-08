## 1. Database & Migrations

- [ ] 1.1 Add migration `internal/engine/store/migrations/011_swarm.sql` creating `swarm_subtasks` table per spec, with indexes on `parent_task_id` and `(parent_task_id, status)`
- [ ] 1.2 Add migration `internal/engine/store/migrations/012_agent_role.sql` adding `role TEXT NOT NULL DEFAULT ''`, `parent_task_id TEXT NULL`, `subtask_id TEXT NULL` columns to `agent_sessions`
- [ ] 1.3 Add `Subtask` struct + `MarshalScopeGlobs`/`ParseScopeGlobs` helpers in `internal/engine/store/swarm.go`
- [ ] 1.4 Add CRUD: `CreateSubtask`, `GetSubtask`, `ListSubtasksByParent`, `UpdateSubtaskStatus`, `SetSubtaskBuilderAgent`, `SetSubtaskReviewerAgent`, `DeleteSubtask`

## 2. File Ownership

- [ ] 2.1 Add `github.com/bmatcuk/doublestar/v4` dependency
- [ ] 2.2 Create `internal/engine/swarm/scope.go` with `MatchScope(globs []string, path string) bool` and `ScopeOverlaps(a, b []string, root string) (bool, []string)` walking the working tree
- [ ] 2.3 Unit tests for scope matching: empty scope, file-level, directory-level, doublestar, character classes
- [ ] 2.4 Unit tests for overlap detection: disjoint, nested, identical, file-level vs directory, empty repo

## 3. Swarm Service

- [ ] 3.1 Create `internal/service/swarm.go` with `SwarmService` struct holding store, agent service, event bus, and adapter registry
- [ ] 3.2 Implement `Decompose(ctx, parentID, []SubtaskSpec) error` — single-tx insert, validate parent exists, validate scope syntax
- [ ] 3.3 Implement `ListSubtasks(parentID) ([]Subtask, error)` and `Snapshot(parentID) ([]byte, error)` (JSON)
- [ ] 3.4 Implement `MarkBuilt(subtaskID) error` — transition `building → review`, kill builder agent, spawn reviewer
- [ ] 3.5 Implement `Review(subtaskID, approve bool, notes string) error` — handle both verdicts with appropriate state transitions and agent lifecycle
- [ ] 3.6 Implement `AssignNext(parentID) error` — find queued sub-tasks whose scope no longer conflicts and spawn their agents (called from event handler)
- [ ] 3.7 Subscribe to `EventAgentDied` and call `AssignNext` for the affected parent
- [ ] 3.8 Concurrent agent cap: read `swarm.max_concurrent_agents` from config (default 4); refuse spawn when cap is reached
- [ ] 3.9 Unit tests covering each transition, conflict refusal, auto-spawn on completion, concurrent cap

## 4. Agent Service Changes

- [ ] 4.1 Extend `AgentSpawnOptions` struct with `Role string`, `ParentTaskID string`, `SubtaskID string`, `Scope []string` fields
- [ ] 4.2 Modify `SpawnAgent` to: (a) inject `LEGATO_AGENT_ROLE` and `LEGATO_PARENT_TASK_ID` when set, (b) call `ScopeOverlaps` against active siblings before spawn, (c) call `RoleSystemPrompt` on the configured adapter and pass to launch command
- [ ] 4.3 Persist `role`, `parent_task_id`, `subtask_id` columns on `agent_sessions` insert
- [ ] 4.4 `ReconcileSessions` preserves new columns when transitioning to `dead`
- [ ] 4.5 Unit tests: spawn with role + scope, refuse on conflict, role columns persisted, env vars set

## 5. Adapter Interface Extension

- [ ] 5.1 Add optional `RoleSystemPrompt(role string) string` to `AIToolAdapter` interface (Go uses interface assertion at call site, not method on interface — define `type RolePromptingAdapter interface { RoleSystemPrompt(string) string }`)
- [ ] 5.2 Implement `RoleSystemPrompt` on `ClaudeCodeAdapter` with built-in prompts for `coordinator|builder|scout|reviewer` (read from embedded markdown files under `internal/engine/hooks/prompts/`)
- [ ] 5.3 Implement `RoleSystemPrompt` on `OpenCodeAdapter` (delegate to embedded prompts via plugin context)
- [ ] 5.4 Implement `RoleSystemPrompt` on `ChimeraAdapter`
- [ ] 5.5 Add config override resolution: `swarm.prompts.<role>.<adapter>` map in `config.yaml`; adapter checks config first
- [ ] 5.6 Adapter tests verifying each role returns a non-empty prompt and unknown role returns empty string

## 6. CLI Subcommands

- [ ] 6.1 Create `internal/cli/swarm.go` with `Decompose`, `Status`, `Review`, `Assign`, `Built` handlers
- [ ] 6.2 Wire `swarm` subcommand group in `cmd/legato/main.go` with subcommands `decompose|status|review|assign|built`
- [ ] 6.3 `decompose` accepts `--from-file <path>` (JSON or YAML auto-detect) and repeated `--subtask "title:scope:role"`
- [ ] 6.4 `status` outputs JSON snapshot to stdout
- [ ] 6.5 `review` accepts `--approve` / `--reject` with optional `--notes`
- [ ] 6.6 IPC broadcast on every state change (new message type `swarm_changed`)
- [ ] 6.7 Integration tests for each CLI subcommand (using temp DB)

## 7. TUI Overlays

- [ ] 7.1 Create `internal/tui/overlay/swarm.go` — decomposition overlay with row list, per-row inputs (title/scope/role), `tab` to add row, `shift+tab` to remove, `enter` submit
- [ ] 7.2 Wire `s` keybinding on board view to open swarm overlay for selected task
- [ ] 7.3 Add `overlaySwarm` to `overlayType` enum and `activeOverlay` dispatch
- [ ] 7.4 Emit `SwarmDecomposeMsg` on submit; app calls `SwarmService.Decompose`

## 8. TUI Detail View — Swarm Section

- [ ] 8.1 Add swarm graph rendering to `internal/tui/detail/` — list each sub-task with status icon, role, title, scope, assigned agent
- [ ] 8.2 Add navigation within swarm section (`j`/`k` highlights sub-task; `enter` opens sub-task detail)
- [ ] 8.3 Add review keybindings (`a`=approve, `r`=reject) when focused sub-task is in `review` state
- [ ] 8.4 Sub-task detail view (sub-component) shows description, builder logs link, reviewer notes

## 9. TUI Board — Swarm Indicators

- [ ] 9.1 Add `SwarmStats` field to `CardData` — `{Total int, Done int, InReview int, Building int}`
- [ ] 9.2 Render swarm badge on cards: `1/3 ◊` (done/total + swarm icon)
- [ ] 9.3 Populate `SwarmStats` during `DataLoadedMsg` via `SwarmService.ListSubtasks` per swarm parent

## 10. TUI Agents — Coordination Panel

- [ ] 10.1 Add third panel to agent split-view rendering coordination snapshot when focused agent has `parent_task_id`
- [ ] 10.2 Refresh panel on `swarm_changed` IPC message and `EventAgentDied` event

## 11. Wiring & Configuration

- [ ] 11.1 Add `swarm.max_concurrent_agents` (int, default 4) and `swarm.prompts` (map) to config struct in `config/`
- [ ] 11.2 Wire `SwarmService` in `cmd/legato/main.go` after `AgentService`; pass to `NewApp`
- [ ] 11.3 Update `NewApp` signature to accept `SwarmService` (nil-safe)
- [ ] 11.4 Update CLAUDE.md / docs/claude/packages.md with the new package and concepts

## 12. Documentation

- [ ] 12.1 Add `docs/claude/swarm.md` describing the swarm lifecycle, scope semantics, and CLI usage
- [ ] 12.2 Update `docs/claude/dev-notes.md` with swarm-specific testing patterns
- [ ] 12.3 Add `@docs/claude/swarm.md` reference to project CLAUDE.md
- [ ] 12.4 Update help overlay (`?` key) with new swarm keybindings
