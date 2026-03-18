## Context

Legato needs a foundational infrastructure layer before any business logic (service layer) or user interface (TUI) can be built. The engine layer provides the three pillars that every higher layer depends on: SQLite persistence for local state, YAML configuration parsing for user settings, and an in-process event bus for decoupling state changes from consumers.

This is Phase 1 of the Legato development plan. The engine layer sits at the bottom of the architecture stack (`internal/engine/` and `config/`), importing only the standard library and third-party dependencies -- never the service or TUI layers. Building this first establishes stable, tested infrastructure with clear contracts that the service and TUI layers can develop against.

## Goals

- Implement SQLite database initialization and schema migrations for `tickets`, `column_mappings`, and `sync_log` tables.
- Implement CRUD operations for tickets, column mappings, and sync log entries via sqlx.
- Build a YAML configuration parser that resolves XDG-compliant config paths, expands environment variables, and applies sensible defaults.
- Build an in-process event bus using Go channels with publish, subscribe, and unsubscribe semantics.
- Create a minimal `cmd/legato/main.go` that validates the engine layer works end-to-end (insert/read tickets via the store).

## Non-Goals

- Service-layer business logic (BoardService, SyncService) -- that is Phase 2.
- TUI rendering or any Bubbletea/Lipgloss/Glamour code -- that is Phase 3.
- Jira REST API client or ADF-to-Markdown conversion -- that is Phase 4.
- First-run setup wizard or interactive configuration prompts.
- Web UI, HTTP server, or WebSocket support.

## Decisions

### SQLite driver: modernc.org/sqlite

Use `modernc.org/sqlite`, a pure-Go SQLite implementation with no CGO dependency. This simplifies cross-compilation and produces a single static binary. The `github.com/jmoiron/sqlx` package wraps `database/sql` for struct scanning and named parameter binding, reducing boilerplate in CRUD operations.

Trade-off: `modernc.org/sqlite` is slower than CGO-based `mattn/go-sqlite3` for write-heavy workloads. This is acceptable because Legato's local database handles a small volume of tickets (hundreds, not millions) and the performance target is sub-100ms for all local operations.

### Query building: sqlx

Use `sqlx` for all database operations. It provides `StructScan`, `NamedExec`, and `Get`/`Select` which eliminate manual row scanning while staying close to raw SQL. No ORM -- queries are explicit SQL strings, easy to read and debug.

### Configuration: YAML with os.ExpandEnv

Parse `~/.config/legato/config.yaml` using `gopkg.in/yaml.v3`. Environment variable references like `${LEGATO_JIRA_TOKEN}` are expanded using `os.ExpandEnv` on the raw YAML bytes before unmarshalling. This avoids custom template syntax while supporting secret injection from the environment.

Config path resolution follows XDG Base Directory Specification:
1. If `$LEGATO_CONFIG` is set, use that path directly.
2. If `$XDG_CONFIG_HOME` is set, use `$XDG_CONFIG_HOME/legato/config.yaml`.
3. Otherwise, use `~/.config/legato/config.yaml`.

When no config file exists, the parser returns a default configuration struct with reasonable zero values. It does not create a file or prompt the user -- that is the setup wizard's job in a later phase.

### Event bus: Go channels

Implement the `EventBus` interface from spec.md section 3.5 using Go channels. Each subscriber receives a buffered channel. `Publish` fans out events to all subscribers of that event type by sending on each channel in a goroutine. `Subscribe` returns a new channel for a given `EventType`. `Unsubscribe` removes and closes the channel.

The bus uses a `sync.RWMutex` to protect the subscriber map. Buffered channels (buffer size 64) prevent slow subscribers from blocking publishers. If a subscriber's buffer is full, the event is dropped for that subscriber (logged, not fatal).

### Database path

The SQLite database file location is resolved in the following order:
1. If `db.path` is set in the config file, use that.
2. If `$XDG_DATA_HOME` is set, use `$XDG_DATA_HOME/legato/legato.db`.
3. Otherwise, use `~/.local/share/legato/legato.db`.

The parent directory is created automatically if it does not exist.

### Schema migrations

Migrations are embedded in the Go binary using `embed.FS`. On startup, the store checks a `schema_version` pragma or user_version and applies any unapplied migrations in order. The initial migration creates the three tables and indexes from spec.md section 4.1.

## Risks / Trade-offs

### modernc.org/sqlite performance

Pure-Go SQLite is approximately 2-3x slower than the CGO variant for bulk operations. For Legato's workload (small number of tickets, simple queries), this is not a concern. If performance becomes an issue in future phases with large sync operations, switching to CGO-based SQLite is a drop-in replacement at the `database/sql` driver level.

### Channel-based event bus limitations

Go channels work well for in-process pub/sub but do not natively span network boundaries. When the web UI is added in a future version (v4), the event bus will need to fan out over WebSockets. Because the `EventBus` interface is defined separately from its implementation, the channel-based implementation can be replaced with a network-capable one without changing any consumers. This is an acceptable trade-off for v0.

### Dropped events on slow subscribers

Using buffered channels means events can be dropped if a subscriber does not consume them fast enough. For the current use case (TUI re-rendering), this is acceptable -- a missed intermediate state is fine as long as the final state arrives. If reliable delivery becomes necessary, the implementation can be swapped behind the same interface.

### Environment variable expansion timing

Running `os.ExpandEnv` on raw YAML bytes before parsing means environment variables are expanded once at load time. If an environment variable is unset, it expands to an empty string silently. The config validation step after parsing catches required fields that ended up empty and returns clear error messages.
