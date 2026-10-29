## ADDED Requirements

### Requirement: Credentials in configuration
Each entry of `credentials` SHALL have a unique `name`, a `secret` reference, an optional `profile` (default `secret`) and optional `policy` rules that replace the profile's defaults. Legato SHALL refuse to load a configuration whose credential names an unknown profile, an unknown store scheme, or a secret type the profile does not accept.

#### Scenario: Unknown profile
- **WHEN** a credential names profile `nope`
- **THEN** loading the configuration SHALL fail with an error that names the credential and the profile

#### Scenario: Default profile
- **WHEN** a credential has no profile
- **THEN** it SHALL use the shipped `secret` profile and its default rule

### Requirement: The most specific rule applies
For a request or use, Legato SHALL apply the matching rule with the highest specificity: one point when the rule names operations, plus the number of literal characters in its resource pattern. An earlier rule SHALL win a tie. A request or use that no rule matches SHALL be refused.

#### Scenario: Narrow rule beats broad rule
- **WHEN** one rule allows resource `github://cpave3/*` with `auto` and another asks for `github://cpave3/legato` with `request`
- **THEN** a use of `github://cpave3/legato` SHALL follow the `request` rule

#### Scenario: Nothing matches
- **WHEN** no rule matches the requested operation
- **THEN** the request SHALL be refused and recorded

### Requirement: Approval modes
A rule's `approve` SHALL be one of `auto` (grant without asking, until the agent ends), `session` (ask once, until the agent ends), `window` (ask once, until the window ends), `request` (ask for each use) or `deny` (refuse). The approval card SHALL offer the rule's mode and every stricter one, and never a looser one.

#### Scenario: Session grant
- **WHEN** the person approves a `session` request
- **THEN** the agent's later uses within the scope SHALL be allowed without asking until the agent ends

#### Scenario: Request mode
- **WHEN** a rule says `request`
- **THEN** each use SHALL need its own approval

### Requirement: Scoped grants
A grant SHALL cover the operations and resources that were requested and approved, for one execution. Each use SHALL be checked against the execution's live grants. A use outside every grant SHALL become a request for exactly that use.

#### Scenario: Use outside the grant
- **WHEN** an agent with a grant for resource `a` uses resource `b`
- **THEN** the use SHALL NOT be allowed by that grant
- **AND** a request for `b` SHALL be made

#### Scenario: Grant ends with the agent
- **WHEN** the agent's execution ends
- **THEN** its grants SHALL no longer allow any use

### Requirement: Requesting access
The MCP tool `request_access` and the command `legato credential request` SHALL take a credential, operations, resources, a reason and whether the agent is blocked. They SHALL wait up to 60 seconds for a decision. They SHALL return the grant when the person decides in that time, and otherwise `pending` with the approval ID and an instruction to continue other work, or to stop and end the turn when blocked.

#### Scenario: Answered in time
- **WHEN** the person approves within 60 seconds
- **THEN** the tool SHALL return the grant

#### Scenario: Not answered in time
- **WHEN** 60 seconds pass without a decision
- **THEN** the tool SHALL return `pending` and the approval ID
- **AND** the approval SHALL stay open

#### Scenario: Decided later
- **WHEN** the person decides a pending credential approval
- **THEN** Legato SHALL send the agent a message that starts with `[legato approvals]` and states the decision and the scope

### Requirement: Getting a value
`legato credential get <name>` SHALL print the credential's value only when a live grant or an `auto` rule allows the use, waiting as a request does. `legato credential exec <name> --env VAR -- <command>` SHALL run the command with the value in `VAR` only, under the same check.

#### Scenario: No grant
- **WHEN** no grant allows the use and the person does not answer within 60 seconds
- **THEN** the command SHALL print nothing on stdout and SHALL exit with code 4

### Requirement: Limits
A rule MAY set `limit` as a count per period (`requests: 100/day`). Past the limit, a use SHALL become a request even inside a grant.

#### Scenario: Over the limit
- **WHEN** an agent's uses under an `auto` rule reach its limit for the day
- **THEN** the next use SHALL need an approval
