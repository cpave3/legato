## ADDED Requirements

### Requirement: Serve subcommand dispatch

The CLI dispatcher SHALL recognize `serve` as a subcommand and route it to the serve handler. The usage help SHALL include `serve` in the list of available commands.

#### Scenario: Dispatch serve command

- **WHEN** a user runs `legato serve`
- **THEN** the CLI dispatches to the serve handler, not the TUI

#### Scenario: Serve in help text

- **WHEN** a user runs `legato` with an unknown command
- **THEN** the error message includes `serve` in the usage: `usage: legato [task|agent|hooks|serve]`
