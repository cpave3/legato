## ADDED Requirements

### Requirement: Scope as glob patterns

A sub-task MAY declare a file scope as one or more glob patterns matched against working-directory-relative paths. Patterns SHALL follow doublestar syntax (`**`, `*`, `?`, character classes).

#### Scenario: Scope is optional

- **WHEN** a plan entry omits the `scope` field
- **THEN** the sub-task SHALL be persisted with empty scope_globs
- **AND** spawn-time conflict checks SHALL be skipped for that sub-task

#### Scenario: Scope is working-directory-relative

- **WHEN** a sub-task has scope `["api/**"]` for a swarm with working directory `/home/user/Projects/myapp`
- **THEN** the system SHALL interpret the glob as matching files under `/home/user/Projects/myapp/api/**`

#### Scenario: Multiple disjoint scopes

- **WHEN** a sub-task has `scope: ["web/src/**", "web/public/**"]`
- **THEN** the scope SHALL match the union of files under both subdirectories

#### Scenario: Glob syntax validated at plan time

- **WHEN** a plan specifies a malformed glob (e.g. `[`)
- **THEN** plan validation SHALL reject it with a syntax error

### Requirement: Advisory overlap detection

The system SHALL provide `ScopeOverlaps(a, b []string, root string) (bool, []string)` that walks `root` once and returns whether two scope sets currently match a common file. Detection is best-effort over the present working tree.

#### Scenario: Disjoint scopes

- **WHEN** scopes `["api/**"]` and `["web/**"]` are checked against a tree where these directories share no files
- **THEN** the function SHALL return `false, nil`

#### Scenario: Overlapping directories

- **WHEN** scopes `["src/**"]` and `["src/lib/**"]` are checked
- **AND** at least one file exists under `src/lib/`
- **THEN** the function SHALL return `true` with the overlap list including the matching files

#### Scenario: Empty scope returns nothing

- **WHEN** either scope is empty
- **THEN** the function SHALL return `false, nil`

#### Scenario: Tree walk skips noise

- **WHEN** the walk encounters directories named `.git`, `node_modules`, `vendor`, `dist`, `build`, `.next`, `.svelte-kit`, `.turbo`, `.cache`
- **THEN** those directories SHALL be skipped (not recursed)

### Requirement: Spawn-time conflict reporting

When a worker is dispatched, the system SHALL check its scope against active sibling workers in the same swarm. Conflicts SHALL be reported to the conductor via send-keys but SHALL NOT block the spawn unless `cfg.Swarm.StrictScope` is true.

#### Scenario: Conflict in non-strict mode

- **WHEN** `cfg.Swarm.StrictScope` is false (default)
- **AND** a dispatched sub-task's scope overlaps an active sibling
- **THEN** the worker SHALL be spawned anyway
- **AND** the system SHALL deliver `[swarm event] scope warning: worker "<title>" overlaps with active sibling "<sibling-title>" on <N> files` to the conductor's pane

#### Scenario: Conflict in strict mode

- **WHEN** `cfg.Swarm.StrictScope` is true
- **AND** a dispatched sub-task's scope overlaps an active sibling
- **THEN** the spawn SHALL be refused
- **AND** the sub-task SHALL remain `queued`
- **AND** the system SHALL deliver `[swarm event] dispatch refused: scope conflict with active sibling "<sibling-title>"` to the conductor's pane

#### Scenario: Empty repository or non-existent dir

- **WHEN** the working directory is empty or doesn't exist
- **THEN** `ScopeOverlaps` SHALL return `false, nil` and the spawn SHALL proceed

### Requirement: Scope persistence

The `scope_globs` column on `swarm_subtasks` SHALL be a JSON array of strings.

#### Scenario: Storing globs

- **WHEN** a sub-task is dispatched with scope `["api/**", "go.mod"]`
- **THEN** the column SHALL contain `["api/**","go.mod"]`

#### Scenario: Empty scope persisted as empty array

- **WHEN** a sub-task has no scope
- **THEN** the column SHALL contain `[]` (not NULL)
