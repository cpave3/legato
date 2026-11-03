## ADDED Requirements

### Requirement: Finding st
The updater SHALL look after the `st` that Legato's own `PATH` finds, following links to the file. It SHALL look after it only when `st version --json` reports `release: true`; a `dev` or `go install` build, or an `st` that has no `version` command, SHALL be reported as not managed and never changed. No `st` on the `PATH` SHALL be reported as missing; Legato SHALL NOT install one.

#### Scenario: Release build of st
- **WHEN** `~/.local/bin/st` is a release build and is on Legato's `PATH`
- **THEN** the updater SHALL look after it

#### Scenario: st built from a checkout
- **WHEN** `st version --json` reports `release: false`
- **THEN** the updater SHALL report it as not managed and SHALL NOT run `st update`

### Requirement: Checking and installing st
Whenever the updater checks for a Legato release (on schedule, when the mode changes, or when asked), it SHALL also run `st update --check --json` for a managed `st`. In `auto` mode an available `st` release SHALL be installed at once with `st update`, without waiting for agents to be quiet: `st` replaces its file by renaming, so running `st` processes are not disturbed. In `ask` mode it SHALL be offered, and installed when the person asks. In `off` mode nothing SHALL be checked or installed. Each `st` command SHALL be given at most 2 minutes. A failure SHALL be recorded with the command's last line of output and SHALL NOT affect Legato's own update.

#### Scenario: Automatic mode
- **WHEN** the mode is `auto` and `st update --check --json` reports `available: true`
- **THEN** the updater SHALL run `st update` and report the new version

#### Scenario: Ask mode
- **WHEN** the mode is `ask` and an `st` release is available
- **THEN** the updater SHALL report it as available and install it only on `POST /api/updates/tools/st/apply`

#### Scenario: st update fails
- **WHEN** `st update` exits non-zero
- **THEN** the tool's state SHALL be `failed` with the reason, and Legato's own update state SHALL be unchanged

### Requirement: Reporting st's updates
`service.UpdateStatus` (and so `GET /api/updates` and `GET /api/settings`'s `updates`) SHALL carry `tools`: for `st`, `{"name", "path", "managed", "version", "latest", "available", "state", "error", "checked_at"}`, `state` one of `missing`, `unmanaged`, `unchecked`, `up_to_date`, `available`, `installing`, `failed`. `POST /api/updates/tools/st/apply` SHALL install it (`202`), answering `409` when it is not managed or nothing newer is available. Settings › Updates SHALL show `st`'s version and state, and in `ask` mode an **Update st** button when one is available. The `updates` Attention producer SHALL add an item for an available `st` release in `ask` mode, whose action installs it.

#### Scenario: Status carries st
- **WHEN** `GET /api/updates` is requested and a managed `st` is up to date
- **THEN** `tools` SHALL contain `st` with `state: "up_to_date"` and its version

#### Scenario: Attention in ask mode
- **WHEN** the mode is `ask` and an `st` release is available
- **THEN** Attention SHALL list it with an action that installs it

#### Scenario: Apply an unmanaged st
- **WHEN** `POST /api/updates/tools/st/apply` is requested and `st` is a `dev` build
- **THEN** the response SHALL be `409` saying it is not a release build
