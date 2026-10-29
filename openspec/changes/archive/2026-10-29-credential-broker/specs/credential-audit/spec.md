## ADDED Requirements

### Requirement: Every credential event is recorded
Legato SHALL append a row to the credential audit log for every request, pending wait, grant, denial, use, refusal, issue and expiry. Each row SHALL record the time, the event, the execution, the task, the agent tool, the workspace, the credential, the operation, the resource, any amount, any approval ID and who decided. A row SHALL NOT contain a secret value.

#### Scenario: Use recorded
- **WHEN** an agent reads a credential under a grant
- **THEN** a `use` row SHALL name the execution, the credential, the operation and the resource

### Requirement: The log is a hash chain
Each row SHALL hold the hash of the row before it and its own hash, the SHA-256 of the previous hash and the row's canonical JSON. `legato audit verify` SHALL report the first row whose hash does not match.

#### Scenario: Edited row
- **WHEN** a row's resource is changed in the database
- **THEN** `legato audit verify` SHALL fail and name that row

#### Scenario: Two processes append
- **WHEN** two Legato processes append rows at the same time
- **THEN** the chain SHALL stay valid

### Requirement: Reading the log
`legato audit [--credential <name>] [--task <id>] [--execution <id>] [--limit <n>] [--json]` SHALL print rows newest first. The web server SHALL serve the same as `GET /api/credentials/audit`.

#### Scenario: Filter by task
- **WHEN** the person runs `legato audit --task t1`
- **THEN** only rows of task `t1` SHALL be printed
