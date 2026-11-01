## ADDED Requirements

### Requirement: Owner lookup
`GET /api/integrations/owner` with scope `owner:read` SHALL take `repo` (`owner/name`) and at least one of `pr`, `branch` and `sha`, and SHALL return the tasks that are not archived and whose code context names that repository and match:
- the linked PR number;
- the head SHA (a prefix of at least 7 characters matches);
- the branch.

The answer SHALL be `{matches: [...]}`, ranked by the best rule matched in that order, then tasks with a running agent first, then the most recently updated. With no match it SHALL be 404 `not_found`. A request without `repo`, or with `repo` alone, SHALL be 400 `bad_request`.

#### Scenario: PR matched before discovery
- **WHEN** a task's code context has branch `feat-x` in `acme/widget`, discovery has not yet linked PR 12, and Argus asks with `repo=acme/widget&pr=12&branch=feat-x`
- **THEN** the task SHALL be returned with `match` `branch`

#### Scenario: PR number ranks first
- **WHEN** one task has PR 12 linked and another only has the same branch
- **THEN** the task with PR 12 SHALL be first, with `match` `pr`

#### Scenario: Nothing owns it
- **WHEN** no task names the repository
- **THEN** the answer SHALL be 404 with `error` `not_found`

### Requirement: What a match holds
Each match SHALL hold:
- `task`: `{id, title, status, url}`, where `status` is the column name, and `url` is present only when `web.public_url` is set;
- `agents`: the task's agent sessions as `{id, kind, state}`, where `id` is the execution ID and `state` is `working`, `waiting`, `idle` or `error` for a running session, or `stopped` for one with a saved conversation and no running session;
- `worktree: {branch}` when a branch is known;
- `pr: {number, url, state}` when one is linked;
- `match`.

A task with neither a running session nor a saved conversation SHALL have `agents: []`. A match SHALL NOT include working directories, descriptions, notes or terminal output.

#### Scenario: Agent waiting for the person
- **WHEN** the owning task's agent is showing a permission request
- **THEN** its state SHALL be `waiting`

#### Scenario: Stopped agent
- **WHEN** the owning task's agent has ended and its conversation was saved
- **THEN** it SHALL be listed with state `stopped`
