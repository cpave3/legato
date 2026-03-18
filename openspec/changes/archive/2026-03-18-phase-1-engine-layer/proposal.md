## Why

Legato needs a foundational infrastructure layer before any business logic or UI can be built. The engine layer provides SQLite persistence, YAML configuration parsing, and an event bus — the three pillars that every higher layer depends on. Building this first enables the service and TUI layers to develop against stable, tested infrastructure with clear contracts.

## What Changes

- Create SQLite database setup with schema migrations for `tickets`, `column_mappings`, and `sync_log` tables
- Implement ticket CRUD operations via sqlx
- Implement column mapping CRUD operations
- Build YAML configuration parser for `~/.config/legato/config.yaml` with environment variable expansion
- Build an in-process event bus using Go channels with publish/subscribe/unsubscribe semantics
- Create a minimal `cmd/legato/main.go` that validates the engine layer works end-to-end

## Capabilities

### New Capabilities
- `sqlite-store`: SQLite database initialization, migrations, and CRUD operations for tickets, column mappings, and sync log
- `config-parser`: YAML configuration file parsing with env var expansion, default values, and XDG-compliant file location
- `event-bus`: In-process pub/sub event system using Go channels for decoupling state changes from consumers

### Modified Capabilities
<!-- None — this is the first phase, no existing capabilities -->

## Impact

- New packages: `internal/engine/store/`, `internal/engine/events/`, `config/`
- New dependencies: `modernc.org/sqlite`, `github.com/jmoiron/sqlx`, `gopkg.in/yaml.v3`
- Creates the SQLite database file at runtime (location configurable)
- Reads `~/.config/legato/config.yaml` (creates default if missing)
