## ADDED Requirements

### Requirement: First prompt on agent spawn
`POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` SHALL accept an optional `prompt` string. When it is present and the request starts a new agent, Legato SHALL deliver it as the agent's first message after the session is ready, by the same path as the launch kickoff: over the native endpoint when the adapter binds one, otherwise typed into the pane.

#### Scenario: Spawn with a prompt and a native bridge
- **WHEN** a client posts `{"agent_kind":"claude-code","working_dir":"/srv/x","prompt":"Fix the failing test"}` to `/api/agents/spawn`
- **THEN** Legato SHALL start the agent and respond with success before the prompt is delivered
- **AND** SHALL submit "Fix the failing test" over the session's native endpoint once it binds

#### Scenario: Spawn with a prompt and no native endpoint
- **WHEN** the adapter does not bind a native endpoint
- **THEN** Legato SHALL type the prompt into the pane after the adapter's kickoff delay

#### Scenario: Prompt for an agent that is already running
- **WHEN** `POST /api/tasks/{id}/agent` selects a live agent instead of starting one, and a `prompt` is given
- **THEN** Legato SHALL send the prompt to that agent as a message
- **AND** SHALL NOT start a second agent

#### Scenario: Prompt combined with resume
- **WHEN** a request has `session_policy: "resume"` and a `prompt`
- **THEN** the resumed session SHALL receive the prompt as its next message, without the fresh-session kickoff

#### Scenario: Prompt that is blank
- **WHEN** `prompt` is present but contains only whitespace
- **THEN** Legato SHALL treat the request as if no prompt were given

### Requirement: Built-in job workflow
Legato SHALL ship a built-in workflow named `legato-job` with inputs `prompt` (required text), `repo` (optional `owner/name`), `ref` (optional), and `agent` (optional adapter name). It SHALL run a `repo.ensure` step and then an `agent.run` step whose prompt is the `prompt` input and whose working directory is the job checkout when `repo` is given, or the default directory otherwise.

#### Scenario: Job with a repository
- **WHEN** `legato-job` is started with `prompt` and `repo: acme/widget`
- **THEN** `repo.ensure` SHALL produce a job checkout of `acme/widget` at the default branch or `ref`
- **AND** `agent.run` SHALL start the agent in that checkout with the prompt

#### Scenario: Job without a repository
- **WHEN** `legato-job` is started with only `prompt`
- **THEN** `repo.ensure` SHALL complete without producing a checkout
- **AND** `agent.run` SHALL start in the configured default directory

#### Scenario: Job started through the API
- **WHEN** a client starts `legato-job` through the workflow run endpoint
- **THEN** the response SHALL include the run ID
- **AND** the run SHALL appear in `/workflows` with its steps and the agent's execution

#### Scenario: Unknown agent input
- **WHEN** `agent` names an adapter that is not configured
- **THEN** the run SHALL be rejected at start with an error naming the adapter
