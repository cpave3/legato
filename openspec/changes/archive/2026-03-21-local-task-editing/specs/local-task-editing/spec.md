## ADDED Requirements

### Requirement: Editor resolution

The system SHALL resolve the editor to use for description editing using the following precedence: config `editor` field → `$VISUAL` environment variable → `$EDITOR` environment variable → `vi`.

#### Scenario: Config editor override

- **WHEN** the config file contains an `editor` field with value `nvim`
- **THEN** the system SHALL use `nvim` as the editor regardless of environment variables

#### Scenario: VISUAL environment variable

- **WHEN** no config `editor` is set and `$VISUAL` is set to `code --wait`
- **THEN** the system SHALL use `code --wait` as the editor command

#### Scenario: EDITOR environment variable fallback

- **WHEN** no config `editor` is set and `$VISUAL` is empty but `$EDITOR` is set to `nano`
- **THEN** the system SHALL use `nano` as the editor

#### Scenario: Default to vi

- **WHEN** no config `editor` is set and both `$VISUAL` and `$EDITOR` are empty
- **THEN** the system SHALL use `vi` as the editor

### Requirement: Update task description via service

The `BoardService` SHALL expose an `UpdateTaskDescription(ctx, id, description)` method that updates the description of a local task.

#### Scenario: Successful description update

- **WHEN** `UpdateTaskDescription` is called with a valid local task ID and new description content
- **THEN** the service SHALL update both `description` and `description_md` fields on the task, persist via the store, and publish a cards-refreshed event

#### Scenario: Reject editing remote task description

- **WHEN** `UpdateTaskDescription` is called with a task ID that has a non-nil provider (remote/synced task)
- **THEN** the service SHALL return an error indicating that remote task descriptions cannot be edited locally

#### Scenario: Task not found

- **WHEN** `UpdateTaskDescription` is called with a non-existent task ID
- **THEN** the service SHALL return a not-found error

### Requirement: Config editor field

The config struct SHALL include an optional `editor` string field, parsed from the `editor` key in `config.yaml`.

#### Scenario: Editor field present

- **WHEN** the config file contains `editor: nvim`
- **THEN** `cfg.Editor` SHALL be `"nvim"`

#### Scenario: Editor field absent

- **WHEN** the config file does not contain an `editor` key
- **THEN** `cfg.Editor` SHALL be empty string (zero value)
