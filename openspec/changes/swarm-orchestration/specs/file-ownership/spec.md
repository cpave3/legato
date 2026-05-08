## ADDED Requirements

### Requirement: Scope as glob patterns

A sub-task SHALL declare its file scope as one or more glob patterns matched against repository-relative paths. Patterns SHALL follow the `doublestar` syntax (`**`, `*`, `?`, character classes).

#### Scenario: Single-directory scope

- **WHEN** a sub-task has `scope_globs = ["api/**"]`
- **THEN** the scope SHALL match all files at any depth under `api/`

#### Scenario: Multiple disjoint scopes

- **WHEN** a sub-task has `scope_globs = ["web/src/**", "web/public/**"]`
- **THEN** the scope SHALL match the union of files under both directories

#### Scenario: File-level scope

- **WHEN** a sub-task has `scope_globs = ["go.mod", "go.sum"]`
- **THEN** the scope SHALL match exactly those two files

### Requirement: Overlap detection

The system SHALL provide a `ScopeOverlaps(a, b []string) (bool, []string)` function that returns whether two scope sets overlap and the list of overlapping repository paths.

#### Scenario: Disjoint scopes

- **WHEN** scopes `["api/**"]` and `["web/**"]` are checked against a repository where these directories share no files
- **THEN** the function SHALL return `false, nil`

#### Scenario: Overlapping directories

- **WHEN** scopes `["src/**"]` and `["src/lib/**"]` are checked
- **THEN** the function SHALL return `true` with the overlap list including all files matched by both patterns

#### Scenario: Empty scope

- **WHEN** either scope is empty
- **THEN** the function SHALL return `false, nil` (an empty scope owns nothing)

### Requirement: Spawn-time conflict refusal

`AgentService.SpawnAgent` SHALL refuse to spawn a swarm sub-task agent whose scope overlaps any sibling sub-task currently in `building` state.

#### Scenario: Conflict with active sibling

- **WHEN** sub-task A with scope `["src/**"]` is in `building` state
- **AND** the user attempts to spawn sub-task B with scope `["src/lib/**"]`
- **THEN** the spawn SHALL fail with an error naming the conflicting sibling, and sub-task B SHALL remain in `queued` state

#### Scenario: No conflict with completed sibling

- **WHEN** sub-task A with scope `["src/**"]` has status `done`
- **AND** the user attempts to spawn sub-task B with scope `["src/lib/**"]`
- **THEN** the spawn SHALL succeed

#### Scenario: Non-swarm agent ignores scope

- **WHEN** a non-swarm agent (no `parent_task_id`) is spawned
- **THEN** scope-overlap checks SHALL be skipped (existing single-task spawn behavior preserved)

### Requirement: Automatic sequencing

When a sub-task is decomposed with a scope that overlaps another active sub-task, the system SHALL queue it and SHALL spawn its agent automatically when the overlap clears.

#### Scenario: Queued waits for predecessor

- **WHEN** sub-task A (`scope=["src/**"]`, status `building`) and sub-task B (`scope=["src/lib/**"]`, status `queued`) exist
- **AND** sub-task A transitions to `done`
- **THEN** the system SHALL automatically spawn sub-task B's agent (transitioning B to `building`)

#### Scenario: Manual sub-task override

- **WHEN** the user invokes `legato swarm assign <subtask-id>` to start a queued sub-task
- **AND** the sub-task's scope overlaps an active sibling
- **THEN** the command SHALL fail with the same conflict error as `SpawnAgent`

### Requirement: Scope persistence

The `scope_globs` column on `swarm_subtasks` SHALL be a JSON array of strings.

#### Scenario: Storing globs

- **WHEN** a sub-task is created with `scope = ["api/**", "go.mod"]`
- **THEN** the column SHALL contain `["api/**","go.mod"]` (valid JSON)

#### Scenario: Reading globs

- **WHEN** a sub-task is loaded from the database
- **THEN** the system SHALL parse `scope_globs` into a `[]string` and reject the row if JSON parsing fails
