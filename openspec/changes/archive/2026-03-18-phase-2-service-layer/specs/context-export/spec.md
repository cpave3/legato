## ADDED Requirements

### Requirement: Description-only export format

The system SHALL support an `ExportFormatDescription` format that produces a markdown string suitable for quick paste into an AI agent prompt. The output MUST begin with a level-2 heading containing the issue key and summary, followed by the card's markdown description body.

#### Scenario: Standard card with description

- **WHEN** a card with key "REX-1238", summary "Refactor user service", and a multi-paragraph description is exported with `ExportFormatDescription`
- **THEN** the output SHALL be a markdown string starting with `## REX-1238: Refactor user service` followed by a blank line and the full description body

#### Scenario: Card with empty description

- **WHEN** a card with no description is exported with `ExportFormatDescription`
- **THEN** the output SHALL contain the heading with key and summary, followed by no description body, and SHALL NOT produce an error

### Requirement: Full structured block export format

The system SHALL support an `ExportFormatFull` format that produces a complete context block with all card metadata. The output MUST begin with a level-1 heading `# Ticket: {KEY}`, followed by bold metadata fields (Summary, Type, Priority, Epic, Labels, URL), a horizontal rule separator, and the full description body.

#### Scenario: Standard card with all metadata

- **WHEN** a card with key "REX-1238", summary "Refactor user service", type "Story", priority "High", epic "Platform Modernisation", labels ["backend"], and a URL is exported with `ExportFormatFull`
- **THEN** the output SHALL match the full context block format from spec.md section 8.1, including all metadata fields on separate bold-prefixed lines and the description after a `---` separator

#### Scenario: Card with missing optional metadata

- **WHEN** a card with no epic, no labels, and no URL is exported with `ExportFormatFull`
- **THEN** the output SHALL omit the missing metadata fields rather than rendering empty values, and the description section SHALL still appear after the separator

#### Scenario: Card with empty description in full format

- **WHEN** a card with metadata but no description is exported with `ExportFormatFull`
- **THEN** the output SHALL include the metadata block and separator but no description section after it

### Requirement: ExportFormat type safety

The system SHALL define an `ExportFormat` type (e.g., typed int or string constant) with named constants for each supported format. `ExportCardContext` MUST return an error if called with an unrecognized format value.

#### Scenario: Unknown format rejected

- **WHEN** `ExportCardContext` is called with a format value that is not `ExportFormatDescription` or `ExportFormatFull`
- **THEN** it SHALL return an empty string and an error indicating the format is not supported

### Requirement: Export output is pure markdown

All export formats MUST produce plain markdown text with no terminal escape codes, ANSI sequences, or presentation-layer formatting. The output MUST be suitable for pasting into any plain-text context (clipboard, file, API request).

#### Scenario: Output contains no escape sequences

- **WHEN** any export format is used to format a card
- **THEN** the resulting string SHALL contain no ANSI escape sequences or non-printable characters other than standard whitespace (newline, space, tab)
