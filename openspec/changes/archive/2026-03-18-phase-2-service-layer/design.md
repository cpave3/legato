## Context

The service layer sits between the engine layer (SQLite store, event bus) and the presentation layer (TUI, future web UI). It defines the business logic contracts as Go interfaces that presentation clients consume directly (TUI via Go calls) or indirectly (future web UI via HTTP/WS handlers). Without this layer, business logic would leak into presentation code, violating the headless-core architecture and forcing duplication when a second client is added.

Phase 2 delivers three capabilities: `BoardService` for kanban operations, a stub `SyncService` with fake Jira data, and context export formatting for clipboard/agent use. All three are presentation-agnostic — they know nothing about Bubbletea, Lipgloss, or any rendering concern.

## Goals / Non-Goals

### Goals

- Define `BoardService`, `SyncService`, and `EventBus` interfaces in `internal/service/interfaces.go`.
- Implement `BoardService` backed by the SQLite store from Phase 1: list columns, list/get/move/reorder cards, search cards, and export card context.
- Implement a stub `SyncService` that seeds in-memory fake Jira data, enabling development and testing without Jira credentials.
- Implement context export formatting producing two formats: description-only and full structured block.
- Wire event publishing so that service mutations (card move, reorder, sync) publish events through the `EventBus`.

### Non-Goals

- Real Jira integration — the stub uses fake data; real sync is Phase 4.
- TUI code — no imports of `bubbletea`, `lipgloss`, or `glamour` in this phase.
- Web server or HTTP handlers — the interfaces are designed to support wrapping, but no server code is written.
- Agent orchestration — `AgentService` is a future concern.

## Decisions

### Interface-first design

All service contracts are defined as Go interfaces before any implementation begins. This ensures the TUI (Phase 3) can be developed against the interface without coupling to implementation details, and enables easy substitution (e.g., swapping stub sync for real Jira sync in Phase 4).

The interfaces follow the signatures defined in spec.md section 3.4: `BoardService`, `SyncService`, and `EventBus`.

### Stub SyncService with fake data

Rather than requiring Jira credentials for development, the stub `SyncService` generates realistic fake tickets across all configured columns. It implements the full `SyncService` interface (`Sync`, `Status`, `Subscribe`) so consumers cannot distinguish it from a real implementation. The fake data includes varied priorities, types, labels, and multi-paragraph descriptions with acceptance criteria to exercise the full rendering pipeline.

### Context export formats

Two export formats are supported, matching spec.md section 8.1:

- **Description-only** (`ExportFormatDescription`): Issue key + summary as heading, followed by the markdown description. Optimized for quick paste into an AI agent prompt.
- **Full structured block** (`ExportFormatFull`): Complete metadata block (summary, type, priority, epic, labels, URL) followed by the full description. Optimized for giving an agent maximum context.

Both formats are plain markdown strings returned by `BoardService.ExportCardContext()`. The presentation layer decides what to do with them (clipboard copy, file write, etc.).

### Event publishing on mutations

`BoardService.MoveCard` and `BoardService.ReorderCard` publish `EventCardMoved` and `EventCardUpdated` events respectively through the `EventBus`. `SyncService.Sync` publishes `EventSyncStarted`, `EventSyncCompleted` or `EventSyncFailed`, and `EventCardsRefreshed` on success. This keeps the presentation layer reactive — it subscribes to events and re-renders, rather than polling.

## Risks / Trade-offs

### Interface stability

The interfaces defined now become the contracts that Phase 3 (TUI) and Phase 4 (Jira) build against. Changing them later requires updating all consumers. Mitigation: the interfaces are modeled directly on the spec.md definitions which have been reviewed, and Go interfaces are implicitly satisfied, so adding methods to a new interface (rather than modifying an existing one) is non-breaking.

### Fake data realism

The stub SyncService must produce data realistic enough to exercise all downstream code paths — varied column distributions, multi-line descriptions, edge cases like empty descriptions or long summaries. If the fake data is too simple, bugs will hide until Phase 4 introduces real Jira data. Mitigation: the stub seeds at least 8-10 tickets across all columns with varied metadata, including edge cases (no description, very long summary, many labels).
