## Why

The service layer is the business logic core of Legato — it sits between the engine (infrastructure) and presentation (TUI/web). Without it, the TUI would need to contain business logic directly, violating the headless-core architecture and making a future web UI impossible without duplication. This layer defines the contracts (Go interfaces) that all presentation layers consume.

## What Changes

- Define `BoardService`, `SyncService`, and `EventBus` interfaces in `internal/service/interfaces.go`
- Implement `BoardService`: list columns, list/get/move/reorder cards, search, and export card context
- Implement a stub `SyncService` with in-memory fake Jira data for testing without Jira credentials
- Implement context export formatting (description-only and full structured block) for clipboard/agent use
- Wire event publishing: services publish events on state mutations, consumers subscribe

## Capabilities

### New Capabilities
- `board-service`: BoardService implementation providing kanban operations (list, move, reorder, search, export) against the SQLite store
- `sync-service-stub`: Stub SyncService with fake Jira data for development and testing without external dependencies
- `context-export`: Presentation-agnostic context formatting for clipboard copy (description-only and full structured block formats)

### Modified Capabilities
<!-- None -->

## Impact

- New package: `internal/service/`
- Depends on Phase 1 (engine layer) being complete
- Defines the interface contracts that Phase 3 (TUI) and Phase 4 (Jira) build against
- No external dependencies beyond what Phase 1 introduces
