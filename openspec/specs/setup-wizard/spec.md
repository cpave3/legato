## ADDED Requirements

### Requirement: Prompt for Jira Connection Details

The setup wizard SHALL prompt the user for their Jira base URL and email address on first run. The wizard MUST validate that the base URL is a valid HTTPS URL.

#### Scenario: Valid Jira URL and email

- **WHEN** the user enters a valid Jira base URL (e.g., `https://company.atlassian.net`) and email
- **THEN** the wizard stores these values and proceeds to API token setup

#### Scenario: Invalid Jira URL

- **WHEN** the user enters a URL that is not valid HTTPS
- **THEN** the wizard displays an error and re-prompts for the URL

### Requirement: API Token Instructions

The setup wizard SHALL display instructions for creating a Jira API token, including a link to the Atlassian API token management page (`https://id.atlassian.com/manage-profile/security/api-tokens`). The wizard MUST prompt the user to enter their generated token.

#### Scenario: Token entry

- **WHEN** the wizard reaches the API token step
- **THEN** the wizard displays instructions with the Atlassian token management URL and prompts the user to paste their token

#### Scenario: Token validation

- **WHEN** the user enters an API token
- **THEN** the wizard attempts a test API call to validate the credentials before proceeding

#### Scenario: Invalid token

- **WHEN** the test API call fails with HTTP 401
- **THEN** the wizard displays an authentication error and re-prompts for the token

### Requirement: Project Key Selection

The setup wizard SHALL allow the user to select one or more Jira project keys to sync. The wizard SHOULD list available projects from the Jira API for the user to choose from.

#### Scenario: List available projects

- **WHEN** the wizard reaches the project selection step with valid credentials
- **THEN** the wizard fetches and displays available Jira projects for the authenticated user

#### Scenario: User selects projects

- **WHEN** the user selects one or more project keys
- **THEN** the selected keys are stored in the config as `project_keys`

### Requirement: Workflow Discovery

The setup wizard SHALL fetch the available statuses and transitions for the selected project(s) via the Jira API. The wizard MUST use `GET /rest/api/3/project/{key}/statuses` to enumerate statuses by issue type.

#### Scenario: Discover project statuses

- **WHEN** the wizard fetches statuses for a selected project
- **THEN** the wizard receives a list of all statuses used across issue types in that project

#### Scenario: Multiple issue types with different workflows

- **WHEN** a project has different workflows for different issue types (e.g., Bug vs Story)
- **THEN** the wizard collects the union of all statuses across issue types

### Requirement: Auto-Generate Column Mappings

The setup wizard SHALL automatically generate column mappings by matching discovered Jira statuses to default Legato column names using name heuristics. The wizard MUST present the generated mappings for user confirmation and allow manual adjustment.

#### Scenario: Statuses match default columns

- **WHEN** the discovered statuses include names like "To Do", "In Progress", and "Done"
- **THEN** the wizard maps them to Backlog, Doing, and Done columns respectively

#### Scenario: Statuses do not match defaults

- **WHEN** the discovered statuses have non-standard names that cannot be matched heuristically
- **THEN** the wizard presents the unmatched statuses and prompts the user to assign them to columns

#### Scenario: User confirms or adjusts mappings

- **WHEN** the wizard presents auto-generated column mappings
- **THEN** the user can confirm the mappings as-is or manually reassign statuses to different columns

#### Scenario: Transition ID discovery

- **WHEN** column mappings are confirmed
- **THEN** the wizard discovers and records the transition IDs needed to move issues into each column's target status

### Requirement: Write Config File

The setup wizard SHALL write the completed configuration to `~/.config/legato/config.yaml`. The API token MUST be stored as an environment variable reference (`${LEGATO_JIRA_TOKEN}`) rather than a plaintext value. The wizard MUST instruct the user to set the environment variable.

#### Scenario: Write config on completion

- **WHEN** the wizard completes all steps successfully
- **THEN** a valid YAML config file is written to `~/.config/legato/config.yaml` with all gathered settings

#### Scenario: Token stored as env var reference

- **WHEN** the config file is written
- **THEN** the `api_token` field contains `${LEGATO_JIRA_TOKEN}` and the wizard instructs the user to set `export LEGATO_JIRA_TOKEN=<their-token>` in their shell profile

#### Scenario: Config directory does not exist

- **WHEN** the `~/.config/legato/` directory does not exist
- **THEN** the wizard creates the directory before writing the config file

### Requirement: First-Run Detection

The setup wizard SHALL be triggered automatically when Legato starts and no config file exists at the expected path. The wizard MUST also be invocable manually via a `legato setup` command.

#### Scenario: No config file on startup

- **WHEN** Legato starts and `~/.config/legato/config.yaml` does not exist
- **THEN** the setup wizard is launched automatically

#### Scenario: Manual setup invocation

- **WHEN** the user runs `legato setup`
- **THEN** the setup wizard runs regardless of whether a config file already exists, allowing re-configuration
