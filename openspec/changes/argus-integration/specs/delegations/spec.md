## ADDED Requirements

### Requirement: Accepting a delegation
`POST /api/integrations/delegations` with scope `delegate` SHALL accept `{caseId, mode, target, brief, title?, idempotencyKey}`, where:
- `mode` is `fix` or `find_out`;
- `target` is `{task}`, `{repo, pr, branch?}` or `{repo}`;
- the field limits are those of the design (D3).

It SHALL resolve the target and choose a route before answering, store the delegation, and answer 201 `{delegationId, route, taskId, state}`. Delivery SHALL then happen in the background. A target that cannot be routed SHALL be refused with its error code, and nothing SHALL be stored. Without `integrations.argus` configured, the route SHALL answer 503 `unavailable`.

#### Scenario: Delegation to a task with a running agent
- **WHEN** Argus delegates `fix` to a task whose agent is running
- **THEN** the answer SHALL be 201 with `route` `message` and a `delegationId` starting `dl-`

#### Scenario: Bad mode
- **WHEN** the body's `mode` is `rewrite`
- **THEN** the answer SHALL be 400 `bad_request` and nothing SHALL be stored

### Requirement: Idempotent delegations
Idempotency keys SHALL be scoped to the integration. A request repeating a key with the same body SHALL return the existing delegation with 200 and SHALL NOT deliver again. A request repeating a key with a different body SHALL get 409 `idempotency_conflict`.

#### Scenario: Retried request
- **WHEN** Argus sends the same delegation twice with the same key
- **THEN** both answers SHALL carry the same `delegationId`, and the agent SHALL receive the brief once

#### Scenario: Key reused for another body
- **WHEN** a key is reused with a different brief
- **THEN** the answer SHALL be 409 `idempotency_conflict`

### Requirement: Choosing the route
Legato SHALL route a delegation as follows:
- `{task}` is that task, or 404 `unknown_task`.
- `{repo, pr}` is the best owner-lookup match, or a new task when there is none.
- `{repo}` is always a new task.

For a task, the route SHALL be:
- `message` when it has a running agent in any state;
- `resume` when it has a saved conversation and no running agent;
- `start` when it has neither, in the task's working directory, or 422 `no_checkout` when it has none.

A stopped swarm or workflow agent SHALL be 422 `not_resumable`, with no fallback to `start`. A new task SHALL need the repository in the `repositories` catalog with a `primary_dir`, otherwise 422 `unknown_repo`. A new task for `{repo, pr}`:
- SHALL start in a new worktree on the PR's head branch when a worktree runner is configured and the head is not in a fork;
- otherwise, for `find_out`, SHALL start in `primary_dir`;
- otherwise, for `fix`, SHALL be 422 `no_checkout`.

A started agent SHALL run the tool named by `integrations.argus.agent`.

#### Scenario: Stopped agent resumed
- **WHEN** the target task's agent ended with a saved conversation
- **THEN** the route SHALL be `resume`, and the brief SHALL be the first message after the conversation is resumed

#### Scenario: Fix without a checkout
- **WHEN** Argus delegates `fix` for `acme/widget#12`, no task owns it, and no worktree runner is configured
- **THEN** the answer SHALL be 422 `no_checkout` and no task SHALL be created

#### Scenario: Unknown repository
- **WHEN** the target is `{repo: "acme/other"}` and the catalog has no such repository
- **THEN** the answer SHALL be 422 `unknown_repo`

#### Scenario: Swarm agent
- **WHEN** the target task's stopped agent was a swarm conductor
- **THEN** the answer SHALL be 422 `not_resumable`

### Requirement: The delivered message
The agent SHALL receive `[argus case <caseId> · delegation <id> · <fix|find out>]`, then the brief unchanged, then a footer saying the message is from Argus, not from the person, and to answer with `reply_case` and the delegation ID. The delegation SHALL become `delivered` when the message path accepts the text, or when the kickoff of a resumed or started agent is submitted. A delivery that fails SHALL set `failed` with an error code and SHALL raise the Attention signal `delegation_failed` on the task.

#### Scenario: Brief tagged
- **WHEN** a `find_out` delegation is delivered by `message`
- **THEN** the agent's message SHALL start with `[argus case` and the delegation ID, and contain the brief byte for byte

#### Scenario: Spawn fails
- **WHEN** a `start` route's spawn fails after the delegation was accepted
- **THEN** its state SHALL be `failed` with `spawn_failed`, and Attention SHALL show `delegation_failed` for the task

### Requirement: Enforcing find_out
Legato SHALL enforce `find_out` when it starts or resumes an agent for a `find_out` delegation or its follow-up message:
- it SHALL run the agent in the sandbox with the working directory mounted read-only;
- the session's credential rules SHALL refuse pushes, ref deletions and pull request writes;
- the delegation SHALL record `enforced: true`.

When the route is `message`, or the sandbox cannot run, the delegation SHALL record `enforced: false`.

#### Scenario: Started read-only
- **WHEN** a `find_out` delegation starts a new agent
- **THEN** a write to a file in its working directory SHALL fail, a `git push` SHALL be refused, and the status SHALL show `enforced: true`

#### Scenario: Running agent not enforced
- **WHEN** a `find_out` delegation goes to a running agent
- **THEN** the status SHALL show `enforced: false`

### Requirement: Asking the person first
Legato SHALL wait for the person's decision when `integrations.argus.delivery` is `ask`:
- a delegation SHALL raise an approval of source `delegation` naming the case, mode, task and route, with the start of the brief, and SHALL stay `awaiting_approval` until it is decided, with no timeout;
- Allow SHALL check the route again and deliver;
- Deny SHALL set `failed` with `denied`.

With `auto` (the default), delivery SHALL NOT wait.

#### Scenario: Denied
- **WHEN** the person denies a delegation's approval
- **THEN** its status SHALL be `failed` with error code `denied`, and the agent SHALL receive nothing

### Requirement: Follow-up messages
`POST /api/integrations/delegations/{id}/messages {text, idempotencyKey}` with scope `delegate` SHALL deliver `text` to the delegation's task, by `message` or `resume`, tagged `[argus case <caseId> · delegation <id> · message]` with the same footer. Idempotency SHALL work as for delegations, per delegation. The answer SHALL be:
- 201 `{messageId, state}`;
- 404 `not_found` for another integration's delegation;
- 409 `not_delivered` before the delegation is delivered;
- 409 `closed` when it failed.

#### Scenario: Answer to a question
- **WHEN** an agent replied `question` and Argus posts a message to the delegation
- **THEN** the same task's agent SHALL receive it tagged with the delegation ID

#### Scenario: Before delivery
- **WHEN** Argus posts a message to a delegation that is `awaiting_approval`
- **THEN** the answer SHALL be 409 `not_delivered`

### Requirement: Delegation status
`GET /api/integrations/delegations/{id}` with scope `delegate` SHALL return `{delegationId, state, route, taskId, enforced, createdAt}`, with:
- `agentId` and `deliveredAt` once delivered;
- `error: {code, message}` once failed.

`state` SHALL be `pending`, `awaiting_approval`, `delivered` or `failed`. Another integration's delegation SHALL be 404 `not_found`.

#### Scenario: Delivered
- **WHEN** a delegation was delivered to execution `ex-1`
- **THEN** its status SHALL show `state` `delivered` and `agentId` `ex-1`
