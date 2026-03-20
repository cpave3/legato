## MODIFIED Requirements

### Requirement: Card Rendering

Each card SHALL display the task ID, a truncated title, and visual indicators for priority, agent status, and — when a PR is linked — PR state indicators showing CI check status, review decision, and comment presence.

#### Scenario: Card with PR passing CI and approved

- **WHEN** a card has `pr_meta` with `check_status="pass"` and `review_decision="APPROVED"`
- **THEN** the card SHALL display a green checkmark icon for CI and an approval indicator

#### Scenario: Card with PR failing CI

- **WHEN** a card has `pr_meta` with `check_status="fail"`
- **THEN** the card SHALL display a red X icon for CI status

#### Scenario: Card with PR pending CI

- **WHEN** a card has `pr_meta` with `check_status="pending"`
- **THEN** the card SHALL display a yellow/orange pending icon for CI status

#### Scenario: Card with changes requested

- **WHEN** a card has `pr_meta` with `review_decision="CHANGES_REQUESTED"`
- **THEN** the card SHALL display a warning-colored indicator signaling rework needed

#### Scenario: Card with PR but no checks

- **WHEN** a card has `pr_meta` with `check_status=""`
- **THEN** the card SHALL NOT display any CI icon

#### Scenario: Card with draft PR

- **WHEN** a card has `pr_meta` with `is_draft=true`
- **THEN** the card SHALL display a dimmed/draft indicator instead of review status

#### Scenario: Card with no PR linked

- **WHEN** a card has no `pr_meta`
- **THEN** the card SHALL render identically to current behavior (no PR indicators)

#### Scenario: Card with comments on PR

- **WHEN** a card has `pr_meta` with `comment_count > 0`
- **THEN** the card SHALL display a comment indicator with the count
