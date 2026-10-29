## MODIFIED Requirements

### Requirement: Read-only GitHub boundary

The GitHub read client SHALL expose read operations only and SHALL wrap all HTTP access in a transport that rejects every method other than `GET` before underlying network dispatch. Legato SHALL use `gh` only to retrieve credentials and SHALL NOT execute `gh api`, `gh pr`, or any GitHub mutation command. The only GitHub writes Legato performs SHALL be agents' pushes and pull requests forwarded by the credential broker's HTTP proxy, under the rules of a `github` credential, with a token made for each write (`github-credentials`).

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
- **WHEN** no `github` credential is configured
- **THEN** Legato SHALL perform no GitHub write

### Requirement: Credential secrecy

Credentials SHALL remain in process memory and MUST NOT be persisted, serialized, logged, included in API responses, or included in errors. When a host git process needs a credential, Legato SHALL pass it to that one process through an inherited file descriptor read by an askpass helper, and MUST NOT place it in a command line, environment variable, remote URL, git configuration or file.

#### Scenario: Git clone
- **WHEN** Legato clones a private repository into the cache with a read token from the credential broker
- **THEN** the credential SHALL NOT appear in the git process's arguments or environment, or in any file afterwards

#### Scenario: Error from git
- **WHEN** a host git process fails during a clone or fetch
- **THEN** the error Legato returns SHALL NOT contain the credential
