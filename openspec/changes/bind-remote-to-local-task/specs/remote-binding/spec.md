## ADDED Requirements

### Requirement: Bind remote ticket to local task

The system SHALL allow binding a remote ticket to an existing local task. Binding SHALL set the task's `provider`, `remote_id`, and `remote_meta` fields without changing the task's `id` (primary key). After binding, the task SHALL participate in pull/push sync using the `remote_id` for provider lookups.

#### Scenario: Successful bind

- **WHEN** a user binds a remote ticket (e.g., `REX-123`) to a local task (e.g., `a1b2c3d4`)
- **THEN** the task's `provider` SHALL be set to the provider name, `remote_id` to the remote ticket key, and `remote_meta` to the fetched remote metadata. The task's `id` SHALL remain `a1b2c3d4`. Title and description SHALL be updated from the remote ticket.

#### Scenario: Reject bind on already-bound task

- **WHEN** a user attempts to bind a remote ticket to a task that already has a non-NULL `provider`
- **THEN** the system SHALL return an error indicating the task is already bound to a remote ticket

#### Scenario: Reject bind when remote ticket already tracked

- **WHEN** a user attempts to bind a remote ticket that is already tracked by another task (either as `id` or `remote_id`)
- **THEN** the system SHALL return an error identifying which task already tracks that ticket

#### Scenario: Agent sessions preserved after bind

- **WHEN** a task has active agent sessions and is bound to a remote ticket
- **THEN** all agent sessions, tmux sessions (`legato-<localid>`), state intervals, and PR links SHALL remain valid and unchanged

#### Scenario: Conflict window set on bind

- **WHEN** a remote ticket is bound to a local task
- **THEN** the `remote_meta` SHALL include `local_move_at` set to the current time, giving the standard 5-minute conflict window before remote sync overrides the local column

### Requirement: Store lookup by remote_id

The store SHALL support looking up tasks by `remote_id` in addition to `id`, to support bound tasks where `id != remote_id`.

#### Scenario: Lookup by remote_id

- **WHEN** `GetTaskByRemoteID(provider, remoteID)` is called with a valid provider and remote ID
- **THEN** the store SHALL return the task where `provider` and `remote_id` match, or not-found if none exists

#### Scenario: CLI accepts either ID

- **WHEN** a CLI command (e.g., `legato task update`) is given a task identifier
- **THEN** the system SHALL first try to match by `id`, and if no match is found, SHALL try to match by `remote_id`

### Requirement: Card display shows remote ID when bound

Cards on the board SHALL display the `remote_id` (e.g., `REX-123`) as the visible key when a task has a remote binding, regardless of the internal `id`.

#### Scenario: Bound task card display

- **WHEN** a board card is rendered for a task with `remote_id = "REX-123"` and `id = "a1b2c3d4"`
- **THEN** the card SHALL display `REX-123` as the task key with the appropriate provider icon

#### Scenario: Local task card display unchanged

- **WHEN** a board card is rendered for a task with `remote_id = NULL`
- **THEN** the card SHALL display the `id` as the task key (existing behavior)

### Requirement: Detail view bind trigger

The detail view SHALL support the `i` keybinding on local tasks to open a remote ticket search overlay for binding.

#### Scenario: Press i on local task in detail view

- **WHEN** a user presses `i` while viewing a local task's detail
- **THEN** a search overlay SHALL open allowing the user to search and select a remote ticket to bind

#### Scenario: Press i on remote task in detail view

- **WHEN** a user presses `i` while viewing a remote-tracked task's detail
- **THEN** the keybinding SHALL be a no-op (task is already bound)

#### Scenario: Press i without sync service

- **WHEN** a user presses `i` in the detail view but no sync service is configured
- **THEN** the keybinding SHALL be a no-op (same as board import without sync service)

#### Scenario: Bind confirmation

- **WHEN** a user selects a remote ticket in the bind overlay
- **THEN** the overlay SHALL show a confirmation with the remote ticket's title and key before completing the bind
