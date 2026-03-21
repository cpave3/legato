## MODIFIED Requirements

### Requirement: Task Link Subcommand

The CLI SHALL support `legato task link <task-id> --branch <branch>` to associate a git branch with a task. The command MUST update the task's `pr_meta` in SQLite and broadcast an IPC message to all running TUI instances.

#### Scenario: Link branch to task

- **WHEN** `legato task link abc12345 --branch feature/auth` is executed
- **THEN** the task's `pr_meta` SHALL be set to `{"branch": "feature/auth"}` and an IPC broadcast SHALL notify running instances

#### Scenario: Link with auto-detect branch

- **WHEN** `legato task link abc12345` is executed without `--branch` flag
- **THEN** the CLI SHALL detect the current git branch via `git rev-parse --abbrev-ref HEAD` and use it

#### Scenario: Link to non-existent task

- **WHEN** `legato task link nonexistent --branch foo` is executed
- **THEN** the CLI SHALL exit with an error message indicating the task was not found

#### Scenario: Auto-detect outside git repo

- **WHEN** `legato task link abc12345` is executed without `--branch` and the working directory is not a git repo
- **THEN** the CLI SHALL exit with an error message indicating that `--branch` is required when not in a git repository

### Requirement: Task Unlink Subcommand

The CLI SHALL support `legato task unlink <task-id>` to remove the branch/PR association from a task.

#### Scenario: Unlink branch from task

- **WHEN** `legato task unlink abc12345` is executed for a task with a linked branch
- **THEN** the task's `pr_meta` SHALL be set to NULL and an IPC broadcast SHALL notify running instances

#### Scenario: Unlink task with no branch

- **WHEN** `legato task unlink abc12345` is executed for a task with no linked branch
- **THEN** the CLI SHALL exit successfully with no error (idempotent)
