## ADDED Requirements

### Requirement: Link a branch to a task

The PR tracking service SHALL allow associating a git branch name with any task (local or synced). Linking a branch MUST store the branch name in the task's `pr_meta` JSON field and trigger an immediate PR status fetch for that branch.

#### Scenario: Link branch to a local task

- **WHEN** a user links branch `feature/auth` to local task `abc12345`
- **THEN** the task's `pr_meta` SHALL be updated with `{"branch": "feature/auth"}` and a PR status fetch SHALL be triggered

#### Scenario: Link branch to a Jira-synced task

- **WHEN** a user links a branch to a task with `provider='jira'`
- **THEN** the task's `pr_meta` SHALL be updated independently of `remote_meta` — Jira sync MUST NOT overwrite PR data

#### Scenario: Re-link with a different branch

- **WHEN** a task already has a linked branch and a new branch is linked
- **THEN** the old branch association SHALL be replaced and PR status SHALL be refreshed for the new branch

#### Scenario: Branch already linked to another task

- **WHEN** a branch is linked to a task but is already linked to a different task
- **THEN** the service SHALL proceed with linking (allowing duplicate branch references) since branches may legitimately span tasks

### Requirement: Unlink a branch from a task

The PR tracking service SHALL allow removing a branch association from a task, clearing the `pr_meta` field.

#### Scenario: Unlink an existing branch

- **WHEN** a user unlinks a branch from a task that has one
- **THEN** the task's `pr_meta` SHALL be set to NULL

#### Scenario: Unlink when no branch is linked

- **WHEN** a user attempts to unlink from a task with no linked branch
- **THEN** the service SHALL return without error (idempotent)

### Requirement: Poll PR status on interval

The PR tracking service SHALL periodically fetch PR status for all tasks that have a linked branch. The polling interval MUST be configurable (default 60s). Polling MUST query all linked branches in a single batch operation.

#### Scenario: Periodic poll updates PR state

- **WHEN** the poll interval elapses and 3 tasks have linked branches
- **THEN** the service SHALL batch-fetch PR status for all 3 branches and update each task's `pr_meta` with the latest state

#### Scenario: Poll on app startup

- **WHEN** the application starts and tasks with linked branches exist
- **THEN** the service SHALL perform an initial PR status fetch before the first poll interval

#### Scenario: No tasks with linked branches

- **WHEN** the poll runs but no tasks have linked branches
- **THEN** the service SHALL skip the fetch and wait for the next interval

#### Scenario: gh CLI unavailable during poll

- **WHEN** the `gh` CLI is not available when a poll runs
- **THEN** the service SHALL log a warning and skip the poll cycle without crashing

### Requirement: Publish events on PR status change

The PR tracking service SHALL publish an `EventPRStatusUpdated` event via the event bus after each poll cycle that produces changes. The event MUST include the list of task IDs whose PR status changed.

#### Scenario: PR status changes detected

- **WHEN** a poll cycle finds that 2 of 5 tracked PRs have changed state
- **THEN** the service SHALL publish `EventPRStatusUpdated` with the 2 changed task IDs

#### Scenario: No changes detected

- **WHEN** a poll cycle finds no PR status changes
- **THEN** the service SHALL NOT publish an event

### Requirement: Auto-link branch on agent spawn

The PR tracking service SHALL automatically link the current git branch to a task when an agent session is spawned for that task, if the task does not already have a linked branch.

#### Scenario: Agent spawns with detectable branch

- **WHEN** an agent session is spawned for task `abc12345` and the working directory is on branch `feature/abc`
- **THEN** the service SHALL automatically link `feature/abc` to the task

#### Scenario: Agent spawns but task already has a branch

- **WHEN** an agent session is spawned for a task that already has a linked branch
- **THEN** the service SHALL NOT overwrite the existing branch link

#### Scenario: Agent spawns outside a git repo

- **WHEN** an agent session is spawned but the working directory is not a git repository
- **THEN** the service SHALL skip auto-linking without error

### Requirement: PR status data model

Each task's PR metadata SHALL be stored as a JSON field (`pr_meta`) containing: `branch` (string), `pr_number` (int), `pr_url` (string), `state` (OPEN/MERGED/CLOSED), `is_draft` (bool), `review_decision` (APPROVED/CHANGES_REQUESTED/REVIEW_REQUIRED/""), `check_status` (pass/fail/pending/""), `comment_count` (int), `updated_at` (RFC3339 timestamp of last fetch).

#### Scenario: Full PR metadata stored

- **WHEN** a PR status fetch returns data for a linked branch
- **THEN** all fields in `pr_meta` SHALL be updated with the latest values and `updated_at` set to the current time

#### Scenario: No PR exists for linked branch

- **WHEN** a PR status fetch finds no PR for a linked branch
- **THEN** `pr_meta` SHALL retain the `branch` field but set `pr_number` to 0, `state` to empty, and other PR fields to zero values

### Requirement: Start and stop polling lifecycle

The PR tracking service SHALL expose `StartPolling(ctx)` returning a stop function, matching the pattern used by `SyncService.StartScheduler`. Cancelling the context or calling the stop function MUST cease polling.

#### Scenario: Start polling

- **WHEN** `StartPolling` is called with a context
- **THEN** the service SHALL begin periodic PR status fetches and return a stop function

#### Scenario: Stop polling via stop function

- **WHEN** the returned stop function is called
- **THEN** polling SHALL cease and no further fetches SHALL occur

#### Scenario: Stop polling via context cancellation

- **WHEN** the context passed to `StartPolling` is cancelled
- **THEN** polling SHALL cease gracefully
