## 1. Project Scaffolding

- [x] 1.1 Initialize Go module with path `github.com/cpave3/legato` and create the directory structure: `cmd/legato/`, `internal/engine/store/`, `internal/engine/events/`, `config/`. Validation: `go build ./...` succeeds with no source files yet.
- [x] 1.2 Add third-party dependencies: `modernc.org/sqlite`, `github.com/jmoiron/sqlx`, `gopkg.in/yaml.v3`. Validation: `go mod tidy` succeeds and `go.sum` contains all three modules.

## 2. SQLite Store -- Database Setup

- [x] 2.1 Implement `store.New(dbPath string) (*Store, error)` that opens a SQLite connection via sqlx, enables WAL mode and foreign keys, and creates parent directories if needed. Validation: calling `New` with a path in a non-existent directory creates the directory and database file; calling it again on the same path reuses the existing file.
- [x] 2.2 Embed the initial migration SQL using `embed.FS` that creates `tickets`, `column_mappings`, and `sync_log` tables with all columns and indexes per the schema in spec.md 4.1. Validation: after `New`, querying `sqlite_master` returns all three tables and both indexes.
- [x] 2.3 Implement migration runner that checks `PRAGMA user_version`, applies unapplied migrations in a transaction, and updates `user_version`. Validation: opening a fresh DB sets `user_version` to 1; opening an already-migrated DB applies no changes; a deliberately broken migration rolls back and returns an error.
- [x] 2.4 Implement `Store.Close()` that closes the database connection. Validation: calling `Close` succeeds; subsequent operations return an error.

## 3. SQLite Store -- Ticket CRUD

- [x] 3.1 Define the `Ticket` struct with `db` tags matching all columns in the `tickets` table. Validation: struct compiles and tags match column names.
- [x] 3.2 Implement `CreateTicket(ctx, ticket)` using sqlx `NamedExec`. Validation: inserting a ticket and reading it back returns matching fields; inserting a duplicate ID returns an error.
- [x] 3.3 Implement `GetTicket(ctx, id)` using sqlx `Get`. Validation: returns the correct ticket for a known ID; returns a not-found error for an unknown ID.
- [x] 3.4 Implement `ListTicketsByStatus(ctx, status)` using sqlx `Select`, ordered by `sort_order ASC`. Validation: returns only tickets matching the status, in correct order.
- [x] 3.5 Implement `UpdateTicket(ctx, ticket)` using sqlx `NamedExec`. Validation: modifying fields and calling update persists the changes; `updated_at` reflects the new time.
- [x] 3.6 Implement `DeleteTicket(ctx, id)`. Validation: ticket is removed from the table; deleting a non-existent ID is a no-op (no error).

## 4. SQLite Store -- Column Mapping CRUD

- [x] 4.1 Define the `ColumnMapping` struct with `db` tags matching all columns in `column_mappings`. Validation: struct compiles and tags match column names.
- [x] 4.2 Implement `CreateColumnMapping(ctx, mapping)`, `ListColumnMappings(ctx)` (ordered by `sort_order`), `UpdateColumnMapping(ctx, mapping)`, and `DeleteColumnMapping(ctx, id)`. Validation: full create-list-update-delete cycle works; duplicate `column_name` returns a constraint error.

## 5. SQLite Store -- Sync Log

- [x] 5.1 Define the `SyncLogEntry` struct with `db` tags matching `sync_log` columns. Validation: struct compiles.
- [x] 5.2 Implement `InsertSyncLog(ctx, entry)` (without requiring `created_at`) and `ListSyncLogs(ctx, ticketID)` ordered by `created_at DESC`. Validation: inserting an entry auto-populates `created_at`; listing returns entries in reverse chronological order.

## 6. Config Parser

- [x] 6.1 Define the config structs: `Config`, `JiraConfig`, `BoardConfig`, `ColumnConfig`, `KeybindingsConfig` with `yaml` tags. Validation: struct compiles and YAML tags are correct.
- [x] 6.2 Implement `ResolveConfigPath()` that checks `$LEGATO_CONFIG`, then `$XDG_CONFIG_HOME/legato/config.yaml`, then `~/.config/legato/config.yaml`. Validation: setting `$LEGATO_CONFIG` returns that path; setting `$XDG_CONFIG_HOME` returns the XDG path; with neither set, returns the default path.
- [x] 6.3 Implement `Load() (*Config, error)` that reads the YAML file, runs `os.ExpandEnv` on the raw bytes, unmarshals into the config struct, and applies defaults for missing fields. Validation: a minimal YAML file with only `jira.base_url` parses successfully with defaults filled in; env var references are expanded.
- [x] 6.4 Implement `ResolveDBPath(cfg *Config)` that checks `cfg.DB.Path`, then `$XDG_DATA_HOME/legato/legato.db`, then `~/.local/share/legato/legato.db`. Validation: each precedence level returns the correct path.
- [x] 6.5 Handle missing config file: when the file does not exist, return a config struct with all defaults and no error. Validation: calling `Load` with no config file returns defaults without error.

## 7. Event Bus

- [x] 7.1 Define `EventType` constants and `Event` struct in `internal/engine/events/`. Validation: all six event types from spec.md 3.5 are defined; Event struct has Type, Payload, and At fields.
- [x] 7.2 Implement `New() *Bus` constructor and the `Subscribe(EventType) <-chan Event` method. Subscribe SHALL return a buffered channel (size 64) and register it in the subscriber map. Validation: calling `Subscribe` returns a non-nil channel; subscribing multiple times returns distinct channels.
- [x] 7.3 Implement `Publish(Event)` that fans out to all subscribers of the event's type. Use non-blocking sends (select with default) to drop events when a buffer is full. Validation: published events appear on subscribed channels; events for unsubscribed types do not appear; publishing to a full buffer does not block.
- [x] 7.4 Implement `Unsubscribe(ch <-chan Event)` that removes the channel from the map and closes it. Validation: after unsubscribe, channel is closed and receives no new events; unsubscribing an unknown channel does not panic.
- [x] 7.5 Ensure thread safety with `sync.RWMutex`. Validation: run concurrent publish/subscribe/unsubscribe operations under the race detector (`go test -race`) with no failures.

## 8. Integration Validation

- [x] 8.1 Create `cmd/legato/main.go` that wires together the config parser, store, and event bus. It SHALL: load config, resolve DB path, open the store, subscribe to `EventCardUpdated`, insert a test ticket, publish an event, verify the event is received, read the ticket back, and print the results. Validation: `go run ./cmd/legato` completes without error and prints the inserted ticket and received event.
- [x] 8.2 Ensure `go build ./...` and `go vet ./...` pass with no errors across all packages. Validation: clean build with no warnings.
