## ADDED Requirements

### Requirement: Jira REST API Authentication

The Jira client SHALL authenticate all requests using HTTP Basic Auth with the user's email and API token. The client MUST read credentials from the Legato configuration file. The API token MUST NOT be logged or included in error messages.

#### Scenario: Successful authentication

- **WHEN** the client makes a request with valid email and API token
- **THEN** the request includes an `Authorization` header with Basic auth encoding of `email:api_token`

#### Scenario: Missing credentials

- **WHEN** the client is initialized without an email or API token in config
- **THEN** the client returns a configuration error before making any HTTP requests

#### Scenario: Invalid credentials

- **WHEN** the Jira API responds with HTTP 401
- **THEN** the client returns an authentication error indicating invalid credentials without exposing the token value

### Requirement: Search Tickets via JQL

The Jira client SHALL support searching for issues using JQL queries via `GET /rest/api/3/search`. The client MUST support pagination and MUST request only the fields needed by Legato (summary, status, priority, issuetype, assignee, labels, description, updated, project).

#### Scenario: Search with results

- **WHEN** a JQL query is executed that matches issues
- **THEN** the client returns a list of issues with all requested fields populated

#### Scenario: Search with pagination

- **WHEN** a JQL query matches more issues than the page size (default 50)
- **THEN** the client automatically fetches subsequent pages until all results are retrieved

#### Scenario: Search with no results

- **WHEN** a JQL query matches no issues
- **THEN** the client returns an empty list without error

#### Scenario: Invalid JQL

- **WHEN** a JQL query has invalid syntax and Jira returns HTTP 400
- **THEN** the client returns an error containing the Jira error message describing the JQL problem

### Requirement: Get Issue Detail

The Jira client SHALL support fetching a single issue by key via `GET /rest/api/3/issue/{key}`. The response MUST include the full ADF description, epic link, and browse URL.

#### Scenario: Get existing issue

- **WHEN** an issue key like "REX-1234" is requested
- **THEN** the client returns the full issue detail including summary, description (ADF), status, priority, issue type, assignee, labels, epic key, epic name, and browse URL

#### Scenario: Get non-existent issue

- **WHEN** an issue key that does not exist is requested
- **THEN** the client returns a not-found error

### Requirement: List Available Transitions

The Jira client SHALL support listing available transitions for an issue via `GET /rest/api/3/issue/{key}/transitions`. Each transition MUST include the transition ID, name, and target status.

#### Scenario: List transitions for issue

- **WHEN** transitions are requested for an issue
- **THEN** the client returns all available transitions with their IDs, names, and target status names

#### Scenario: Issue with no available transitions

- **WHEN** transitions are requested for an issue whose workflow has no valid transitions from the current state
- **THEN** the client returns an empty list without error

### Requirement: Execute Transition

The Jira client SHALL support executing a transition on an issue via `POST /rest/api/3/issue/{key}/transitions`. The client MUST send the transition ID in the request body.

#### Scenario: Successful transition

- **WHEN** a valid transition ID is executed on an issue
- **THEN** the Jira API accepts the transition (HTTP 204) and the client returns success

#### Scenario: Invalid transition ID

- **WHEN** a transition ID that is not valid for the issue's current state is executed
- **THEN** the client returns an error indicating the transition is not available

### Requirement: Rate Limit Handling

The Jira client SHALL handle HTTP 429 (Too Many Requests) responses with exponential backoff. The initial backoff MUST be 1 second, doubling on each retry up to a maximum of 60 seconds. The client MUST respect the `Retry-After` header when present.

#### Scenario: Rate limited with Retry-After header

- **WHEN** the Jira API responds with HTTP 429 and a `Retry-After` header of 5 seconds
- **THEN** the client waits at least 5 seconds before retrying the request

#### Scenario: Rate limited without Retry-After header

- **WHEN** the Jira API responds with HTTP 429 without a `Retry-After` header
- **THEN** the client applies exponential backoff starting at 1 second

#### Scenario: Repeated rate limiting

- **WHEN** the client receives consecutive 429 responses
- **THEN** the backoff interval doubles each time (1s, 2s, 4s, 8s, ...) up to a 60-second maximum

#### Scenario: Successful retry after rate limit

- **WHEN** a request succeeds after a rate-limited retry
- **THEN** the backoff interval resets to the initial value for subsequent requests

### Requirement: Request Timeout

The Jira client SHALL enforce a per-request timeout. The default timeout MUST be 30 seconds. Requests that exceed the timeout MUST return a timeout error.

#### Scenario: Request exceeds timeout

- **WHEN** a Jira API request does not complete within 30 seconds
- **THEN** the client cancels the request and returns a timeout error
