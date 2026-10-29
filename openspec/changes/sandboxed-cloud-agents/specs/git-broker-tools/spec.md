## ADDED Requirements

### Requirement: Repository tools in the Legato MCP server
`legato mcp serve` SHALL offer `ensure_repo {repo, ref?}` and `refresh_repo {repo}`, each performed by the host through the broker. The tools SHALL be listed only when a `github` credential is configured. The server SHALL NOT offer a tool that pushes or opens a pull request.

#### Scenario: Tools listed
- **WHEN** a session's MCP client lists tools on a host with a `github` credential
- **THEN** `ensure_repo` and `refresh_repo` SHALL be listed

#### Scenario: Tools not listed
- **WHEN** no `github` credential is configured
- **THEN** neither tool SHALL be listed

#### Scenario: ensure_repo for a sibling
- **WHEN** an agent calls `ensure_repo {repo: "acme/other"}`
- **THEN** the host SHALL ensure the cache and create a checkout under the agent's job directory
- **AND** SHALL return its path

### Requirement: Pushes and pull requests go through the credential broker
A sandboxed agent SHALL push with an ordinary `git push` and open a pull request with a REST request, both through the credential broker's HTTP proxy as `github-credentials` defines: the `git` inspector checks each updated ref against the agent's grant, and `pr.create` is scoped to the head branch. The write token SHALL exist only inside the proxy, for one push or one request.

#### Scenario: Push after asking
- **WHEN** an agent asks with `request_access` for `git.push` on `legato/edu1ugzz-fix-sort`, the person approves, and the agent runs `git push`
- **THEN** the push SHALL reach GitHub

#### Scenario: Push without a grant
- **WHEN** an agent pushes a branch it has no grant for
- **THEN** git SHALL print a `remote:` message that names how to ask with `request_access`
- **AND** nothing SHALL reach GitHub

#### Scenario: No write token in the sandbox
- **WHEN** the sandbox's environment and files are searched for GitHub tokens during a push
- **THEN** no token that can write SHALL be found

### Requirement: Agent working copy is untrusted
No host process that holds a credential SHALL run git with a job clone as its repository or read a job clone's configuration or hooks. A host git process that touches a job clone (the cleanup check, a sibling checkout) SHALL run with system and global git configuration disabled, hooks and fsmonitor off, and no credential in its environment.

#### Scenario: Planted hook
- **WHEN** the job clone contains an executable `.git/hooks/pre-push` and `core.hooksPath` pointing at it
- **THEN** no host process SHALL run the hook

#### Scenario: Planted configuration
- **WHEN** the job clone's `.git/config` sets `core.fsmonitor`, `core.sshCommand`, `credential.helper` or `uploadpack.packObjectsHook` to a command
- **THEN** no host process SHALL run those commands during cleanup or `ensure_repo`

### Requirement: Push policy is the github profile's rules
Which pushes are allowed SHALL be decided by the rules of the `github` credential, as `github-credentials` defines them. With the shipped defaults: a repository not in the credential's `settings.repositories` SHALL be refused; `git.push` on `refs/heads/legato/**` SHALL need one approval per execution; pushes to the default branch, tags and deletions SHALL be refused. Each push SHALL be recorded in the credential audit log and as a task note.

#### Scenario: Agent branch
- **WHEN** an agent pushes `legato/edu1ugzz-fix-sort` for the first time in its execution
- **THEN** Legato SHALL raise one approval request with the mode `session`

#### Scenario: Default branch
- **WHEN** an agent pushes to the repository's default branch
- **THEN** the push SHALL be refused with a message that names the rule

#### Scenario: Repository not listed
- **WHEN** the repository is not in `settings.repositories`
- **THEN** the push SHALL be refused, whether or not the GitHub App could push there

#### Scenario: Recorded
- **WHEN** a push succeeds
- **THEN** a task note SHALL name the repository, branch, and old and new tip
- **AND** the audit log SHALL hold the push with its execution and grant

### Requirement: Pull requests opened through the proxy are tracked
When GitHub answers a `pr.create` request through the proxy with a new pull request, Legato SHALL link that pull request to the agent's task as a pinned PR.

#### Scenario: Pull request opened
- **WHEN** an agent opens a pull request from a granted branch through the proxy
- **THEN** the task's PR tracking SHALL show it
