## ADDED Requirements

### Requirement: Archive keybinding

The board SHALL respond to `X` (shift-x) by initiating the bulk archive flow. If there are done cards, it SHALL open an archive confirmation overlay. If there are no done cards, the keypress SHALL be a no-op.

#### Scenario: X pressed with done cards
- **WHEN** the user presses `X` on the board and `CountDoneCards` returns > 0
- **THEN** an archive confirmation overlay SHALL open showing the count

#### Scenario: X pressed with no done cards
- **WHEN** the user presses `X` on the board and `CountDoneCards` returns 0
- **THEN** nothing SHALL happen

### Requirement: Archive confirmation overlay

The archive overlay SHALL display "Archive N done cards?" with instructions to press `y` to confirm or `n`/`esc` to cancel. On confirmation, it SHALL emit an `ArchiveDoneMsg`. On cancel, it SHALL close without action.

#### Scenario: Confirm archive
- **WHEN** the user presses `y` on the archive overlay
- **THEN** an `ArchiveDoneMsg` SHALL be emitted and the overlay SHALL close

#### Scenario: Cancel archive
- **WHEN** the user presses `n` or `esc` on the archive overlay
- **THEN** the overlay SHALL close and no message SHALL be emitted

### Requirement: Help overlay includes archive keybinding

The help overlay SHALL include the `X` keybinding with description "Archive done cards" in its keybinding reference.

#### Scenario: Archive in help
- **WHEN** the help overlay is displayed
- **THEN** it SHALL list `X` → "Archive done cards" among the board keybindings
