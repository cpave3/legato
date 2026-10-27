## MODIFIED Requirements

### Requirement: Read-only GitHub boundary

The GitHub read client SHALL expose read operations only and SHALL wrap all HTTP access in a transport that rejects every method other than `GET` before underlying network dispatch. Legato SHALL use `gh` only to retrieve credentials and SHALL NOT execute `gh api`, `gh pr`, or any GitHub mutation command. The only GitHub write Legato performs SHALL be through the separate pull-request writer, which is constructed only when `github.write.repositories` is not empty.

#### Scenario: Supported reads
- **WHEN** availability, repository PR lists, PR detail/reviews, commit-associated PRs, checks, or statuses are requested
- **THEN** every dispatched request SHALL use HTTP GET

#### Scenario: Mutation attempt
- **WHEN** POST, PUT, PATCH, DELETE, or another non-GET method reaches the read client's guarded transport
- **THEN** it SHALL return a local read-only invariant error
- **AND** the underlying transport SHALL receive zero requests

#### Scenario: Broad credential scope
- **WHEN** the selected OAuth/PAT credential has write-capable scopes
- **THEN** the read client's GET-only guard SHALL still prevent it from mutating GitHub data

#### Scenario: Writes disabled
- **WHEN** `github.write.repositories` is empty or absent
- **THEN** no pull-request writer SHALL be constructed and Legato SHALL perform no GitHub write

### Requirement: Credential secrecy

Credentials SHALL remain in process memory and MUST NOT be persisted, serialized, logged, included in API responses, or included in errors. When a git process needs the credential, Legato SHALL pass it to that one process through an inherited file descriptor read by an askpass helper, and MUST NOT place it in a command line, environment variable, remote URL, git configuration or file.

#### Scenario: Git clone
- **WHEN** Legato clones a private repository
- **THEN** the credential SHALL NOT appear in the git process's arguments or environment, or in any file afterwards

#### Scenario: Error from git
- **WHEN** a git process fails during a clone, fetch or push
- **THEN** the error Legato returns SHALL NOT contain the credential

## ADDED Requirements

### Requirement: Pull-request writer

`internal/engine/github` SHALL provide a pull-request writer, separate from the read client, with one operation: create a pull request. Its transport SHALL allow only `POST /repos/{owner}/{repo}/pulls` where `{owner}/{repo}` is on the allowlist it was constructed with, and SHALL reject every other method, path and repository before network dispatch.

#### Scenario: Allowed creation
- **WHEN** the writer creates a pull request in an allowlisted repository
- **THEN** exactly one `POST /repos/{owner}/{repo}/pulls` request SHALL be dispatched

#### Scenario: Other repository
- **WHEN** a request for a repository not on the allowlist reaches the writer's transport
- **THEN** it SHALL return a local error and the underlying transport SHALL receive zero requests

#### Scenario: Other endpoint
- **WHEN** a request to any path other than the pulls collection, or with any method other than POST, reaches the writer's transport
- **THEN** it SHALL return a local error and the underlying transport SHALL receive zero requests
