## Requirements

### Requirement: Token-authenticated GitHub API client

The GitHub client SHALL use a configured read-only PAT with GitHub HTTP APIs and SHALL NOT require the `gh` executable or ambient CLI authentication. The token MUST NOT be persisted, serialized, or included in errors.

#### Scenario: Missing token
- **WHEN** no GitHub token is configured
- **THEN** GitHub synchronization SHALL remain disabled with an actionable configuration error

#### Scenario: Authenticated request
- **WHEN** a GitHub read is made
- **THEN** the request SHALL include bearer authentication, the GitHub API version, and the GitHub JSON media type

### Requirement: Repository and PR reads

The client SHALL support repository PR listings, exact commit-to-PR lookup, PR-by-number refresh, review decisions, checks and commit statuses, and comment counts.

#### Scenario: Repository discovery listing
- **WHEN** unresolved tasks in one repository require discovery
- **THEN** the client SHALL return a bounded newest-first PR list suitable for matching all tasks from one response

#### Scenario: Exact commit lookup
- **WHEN** a task has a head SHA
- **THEN** the client SHALL query pull requests associated with that commit

#### Scenario: Linked PR refresh
- **WHEN** a numbered PR is refreshed
- **THEN** the client SHALL return state, draft status, review decision, aggregate check/status state, comments, head branch/SHA/repository, title, and URL

### Requirement: CI aggregation

The client SHALL aggregate both GitHub check runs and legacy commit status contexts: `pass` when all terminal results pass/are neutral/skipped, `fail` when any fails/errors/cancels/times out, `pending` when any is incomplete/expected, and empty when neither mechanism has data.

### Requirement: Classified errors and rate limits

The client SHALL classify authentication, authorization, not-found, rate-limit, server, and malformed-response failures. Rate-limit errors SHALL include reset information when GitHub provides it. API failures MUST NOT be represented as “no PR.”

### Requirement: Local checkout inspection

Local Git infrastructure SHALL inspect an explicitly supplied directory and derive GitHub `owner/repo`, branch, and HEAD SHA from its origin. Unsupported/non-GitHub origins and Git failures SHALL return errors.
