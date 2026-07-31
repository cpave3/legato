## Requirements

### Requirement: Configurable in-memory GitHub credentials

The GitHub runtime SHALL support `auto`, `gh`, and `token` authentication modes. `auto` SHALL prefer an explicit configured token and otherwise retrieve the active GitHub CLI credential by executing exactly `gh auth token` once. `token` SHALL require explicit configuration; `gh` SHALL require CLI authentication.

#### Scenario: Explicit token in auto mode
- **WHEN** `github.auth` is `auto` and `github.token` is non-empty
- **THEN** the configured token SHALL be used and `gh` SHALL not execute

#### Scenario: CLI fallback
- **WHEN** `github.auth` is `auto`, no token is configured, and GitHub CLI is authenticated
- **THEN** Legato SHALL execute `gh auth token` once and retain the trimmed credential in memory

#### Scenario: Authentication unavailable
- **WHEN** the selected provider is missing, unauthenticated, returns empty output, or fails
- **THEN** synchronization SHALL be disabled or the requested remote action rejected with an actionable error mentioning `gh auth login` or `github.token`
- **AND** command output or credential material MUST NOT appear in the error

### Requirement: Read-only GitHub boundary

The GitHub client SHALL expose read operations only and SHALL wrap all HTTP access in a transport that rejects every method other than `GET` before underlying network dispatch. Legato SHALL use `gh` only to retrieve credentials and SHALL NOT execute `gh api`, `gh pr`, or any GitHub mutation command.

#### Scenario: Supported reads
- **WHEN** availability, repository PR lists, PR detail/reviews, commit-associated PRs, checks, or statuses are requested
- **THEN** every dispatched request SHALL use HTTP GET

#### Scenario: Mutation attempt
- **WHEN** POST, PUT, PATCH, DELETE, or another non-GET method reaches the guarded transport
- **THEN** it SHALL return a local read-only invariant error
- **AND** the underlying transport SHALL receive zero requests

#### Scenario: Broad credential scope
- **WHEN** the selected OAuth/PAT credential has write-capable scopes
- **THEN** Legato's GET-only guard SHALL still prevent this integration from mutating GitHub data

### Requirement: Credential secrecy

Credentials SHALL remain in process memory and MUST NOT be persisted, serialized, logged, included in API responses, or included in errors.

### Requirement: Repository and PR reads

The client SHALL support repository PR listings, exact commit-to-PR lookup, PR-by-number refresh, review decisions, checks and commit statuses, and comment counts.

#### Scenario: Repository discovery listing
- **WHEN** unresolved tasks in one repository require discovery
- **THEN** the client SHALL return a bounded newest-first PR list suitable for matching all tasks from one response

#### Scenario: Linked PR refresh
- **WHEN** a numbered PR is refreshed
- **THEN** the client SHALL return state, draft status, review decision, aggregate check/status state, comments, head branch/SHA/repository, title, and URL

### Requirement: CI aggregation

The client SHALL aggregate both GitHub check runs and legacy commit status contexts: `pass` when all terminal results pass/are neutral/skipped, `fail` when any fails/errors/cancels/times out, `pending` when any is incomplete/expected, and empty when neither mechanism has data.

### Requirement: Classified errors and rate limits

The client SHALL classify authentication, authorization, not-found, rate-limit, server, and malformed-response failures. Rate-limit errors SHALL include reset information when GitHub provides it. API failures MUST NOT be represented as “no PR.”

### Requirement: Local checkout inspection

Local Git infrastructure SHALL inspect an explicitly supplied directory and derive GitHub `owner/repo`, branch, and HEAD SHA from its origin. Unsupported/non-GitHub origins and Git failures SHALL return errors.
