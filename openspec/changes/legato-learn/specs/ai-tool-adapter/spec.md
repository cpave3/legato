## ADDED Requirements

### Requirement: Service-composed learning prompt context
The agent service SHALL compose scoped learning context before adapter launch and SHALL deliver equivalent standing context through each adapter's supported mechanism. Adapters SHALL NOT query or mutate learning storage.

#### Scenario: Supported adapters launch with a preference
- **WHEN** the same verified preference is relevant to Chimera, Claude Code, and Codex launches
- **THEN** each agent SHALL receive semantically equivalent learning context and Legato SHALL record a corresponding prompt manifest
