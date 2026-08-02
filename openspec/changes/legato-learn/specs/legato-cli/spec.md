## ADDED Requirements

### Requirement: Learning command family
The CLI SHALL provide a `legato learn` command family for explicit recording, listing, showing, searching, task context, reflection submission, curation decisions, incorporation, verification, consolidation, and prompt-manifest inspection with structured JSON output where applicable.

#### Scenario: Agent submits reflection
- **WHEN** `legato learn reflection submit --request <id> --file <path>` receives valid versioned JSON for an active request
- **THEN** it SHALL persist evidence, advance the waiting workflow step, print structured success, and exit zero

#### Scenario: Invalid callback
- **WHEN** a learning callback is malformed or stale
- **THEN** the CLI SHALL print an actionable error to stderr, exit non-zero, and preserve durable state
