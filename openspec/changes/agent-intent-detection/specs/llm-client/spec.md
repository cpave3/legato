## ADDED Requirements

### Requirement: OpenAI-compatible chat completion client
The system SHALL provide an HTTP client in `internal/engine/llm/` that sends chat completion requests to any OpenAI-compatible API endpoint (`POST /v1/chat/completions`). The client SHALL accept a base URL, model name, and optional API key. The client SHALL return the assistant message content as a string.

#### Scenario: Successful completion request
- **WHEN** the client sends a chat completion request with a system prompt and user message
- **THEN** the client returns the assistant's response content as a string

#### Scenario: API key included when configured
- **WHEN** an API key is provided in the client configuration
- **THEN** the client includes an `Authorization: Bearer <key>` header on all requests

#### Scenario: API key omitted when empty
- **WHEN** no API key is provided (empty string)
- **THEN** the client omits the `Authorization` header (supporting local LLMs that don't require auth)

#### Scenario: Request timeout
- **WHEN** the API endpoint does not respond within the configured timeout
- **THEN** the client returns a timeout error

#### Scenario: Non-200 response
- **WHEN** the API returns a non-200 HTTP status
- **THEN** the client returns an error containing the status code and response body

### Requirement: LLM provider interface
The service layer SHALL define an `LLMProvider` interface with a `Complete(ctx, system, prompt string) (string, error)` method. The engine-layer OpenAI client SHALL be wrapped in an adapter implementing this interface. This allows swapping LLM backends without changing service code.

#### Scenario: Provider abstraction
- **WHEN** the intent service calls `LLMProvider.Complete`
- **THEN** the call is delegated to the underlying engine client's chat completion method with the system string as a system message and the prompt as a user message

### Requirement: LLM configuration
The config parser SHALL support an optional `llm` section with `endpoint` (string), `model` (string), and `api_key` (string, supports env var expansion) fields. When the `llm` section is absent or incomplete, no LLM client SHALL be created.

#### Scenario: Full LLM config present
- **WHEN** config.yaml contains `llm.endpoint` and `llm.model`
- **THEN** the application creates an LLM client with those settings

#### Scenario: LLM config absent
- **WHEN** config.yaml has no `llm` section
- **THEN** no LLM client is created and intent parsing is disabled

#### Scenario: API key with env var expansion
- **WHEN** `llm.api_key` contains `${LEGATO_LLM_API_KEY}` and the env var is set
- **THEN** the expanded value is used as the API key
