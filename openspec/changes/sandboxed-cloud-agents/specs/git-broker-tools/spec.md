## ADDED Requirements

### Requirement: Git tools in the Legato MCP server
`legato mcp serve` SHALL offer `ensure_repo {repo, ref?}`, `refresh_repo {repo}`, `push_branch {repo, branch, force?}` and `open_pr {repo, branch, base?, title, body, draft?}`, each performed by the host through the broker. The tools SHALL be listed only when the host has a GitHub credential, `github.write.repositories` is not empty, and the session is sandboxed or `github.write.unsandboxed` is true.

#### Scenario: Tools listed
- **WHEN** a sandboxed session's MCP client lists tools on a host with a credential and an allowlist
- **THEN** the four git tools SHALL be listed

#### Scenario: Tools not listed
- **WHEN** the allowlist is empty
- **THEN** none of the four git tools SHALL be listed

#### Scenario: ensure_repo for a sibling
- **WHEN** an agent calls `ensure_repo {repo: "acme/other"}`
- **THEN** the host SHALL ensure the cache and create a checkout under the agent's job directory
- **AND** SHALL return its path

### Requirement: Agent working copy is untrusted
To push, the host SHALL read the branch tip from the job clone's ref files without running git there, import the objects into a host-owned bare repository with system and global git configuration disabled and no hooks, verify the received objects, and push from the host-owned repository. No git process that holds the credential SHALL run with the job clone as its repository.

#### Scenario: Planted hook
- **WHEN** the job clone contains an executable `.git/hooks/pre-push` and `core.hooksPath` pointing at it
- **THEN** `push_branch` SHALL push without running the hook

#### Scenario: Planted configuration
- **WHEN** the job clone's `.git/config` sets `core.fsmonitor`, `core.sshCommand`, `credential.helper` or `uploadpack.packObjectsHook` to a command
- **THEN** none of those commands SHALL run during `push_branch`

#### Scenario: Corrupt objects
- **WHEN** the imported objects fail verification
- **THEN** `push_branch` SHALL fail and nothing SHALL be pushed

### Requirement: Push policy
`push_branch` SHALL push only when: the repository is on `github.write.repositories`; the branch name starts with `github.write.branch_prefix` (default `legato/`), which the tool adds when missing; the branch is not the repository's default branch and matches no `github.write.protected` pattern (default `main`, `master`, `release/*`); a force push uses a lease on the tip Legato last pushed to that branch; and no file in the pushed commits exceeds 50 MiB. Each push SHALL be recorded as a task note and in the push log.

#### Scenario: Prefix added
- **WHEN** an agent pushes branch `fix-tests`
- **THEN** Legato SHALL push to `legato/fix-tests` and return that name

#### Scenario: Protected branch
- **WHEN** an agent pushes to `main`, or to a branch that maps to the default branch
- **THEN** the push SHALL be refused with an error that names the rule

#### Scenario: Repository not allowed
- **WHEN** the repository is not on the allowlist
- **THEN** the push SHALL be refused, whether or not the credential could push there

#### Scenario: Force push to a branch Legato did not create
- **WHEN** `force` is true for a branch Legato has no record of pushing
- **THEN** the push SHALL be refused

#### Scenario: Recorded
- **WHEN** a push succeeds
- **THEN** a task note SHALL name the repository, branch, old and new tip, and commit count

### Requirement: Opening a pull request
`open_pr` SHALL create a pull request through the pull-request writer from a branch that Legato pushed for this task, into `base` or the repository's default branch, and SHALL link it to the task as a pinned PR.

#### Scenario: Pull request opened
- **WHEN** an agent calls `open_pr` for a branch it pushed
- **THEN** a pull request SHALL be created and its number and URL returned
- **AND** the task's PR tracking SHALL show it

#### Scenario: Branch not pushed by Legato
- **WHEN** the head branch has no push recorded for this task
- **THEN** `open_pr` SHALL be refused

#### Scenario: Pull request already open
- **WHEN** an open pull request already exists for the head branch
- **THEN** `open_pr` SHALL return the existing pull request instead of failing
