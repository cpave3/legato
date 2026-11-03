## MODIFIED Requirements

### Requirement: Branch convention in the agent guidance
When a `github` credential is configured, `cli.MCPGuidance` and the `conductor.md` and `worker.md` role prompts SHALL tell agents to work on a branch named `legato/<task-id>-<slug>`, to ask with `request_access` for `git.push` on that one branch before the first push, to push with `git push`, and to open pull requests through `LEGATO_GITHUB_API_URL`. When the stack guidance is also given (see `staccato-stacks`), the guidance SHALL add that for a stack the agent asks once, before the first push, for `git.push` on the pattern `github:<owner>/<repo>@refs/heads/legato/<task-id>-*` with its own task ID instead of one branch at a time, and SHALL NOT tell it to ask for one branch only in that case. Without such a credential the guidance SHALL not mention it.

#### Scenario: Guidance with a credential
- **WHEN** an agent is launched and a `github` credential is configured
- **THEN** its guidance SHALL name `legato/<task-id>-<slug>` with its own task ID

#### Scenario: Guidance without a credential
- **WHEN** no `github` credential is configured
- **THEN** the guidance SHALL NOT mention the branch convention

#### Scenario: Guidance with a credential and st
- **WHEN** an agent is launched with a `github` credential configured and the stack guidance given
- **THEN** its guidance SHALL tell it to ask once for `refs/heads/legato/<task-id>-*` with its own task ID before pushing a stack

#### Scenario: Pattern grant covers the stack
- **WHEN** the person has allowed `git.push` on `github:o/r@refs/heads/legato/t1-*` for the session
- **THEN** pushes of `legato/t1-a` and `legato/t1-b`, forced or not, SHALL be allowed without asking again
- **AND** a push of `legato/t2-a` SHALL NOT be covered by that grant
