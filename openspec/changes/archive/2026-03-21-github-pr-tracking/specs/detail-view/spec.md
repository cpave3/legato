## MODIFIED Requirements

### Requirement: Task Metadata Header

The detail view header SHALL display task metadata including ID, status, priority, provider, and — when a PR is linked — a PR status section showing the PR number as a clickable link, approval state, CI check status, and comment count.

#### Scenario: Task with linked PR showing full status

- **WHEN** the detail view opens for a task with `pr_meta` containing a PR
- **THEN** the header SHALL display a "PR" section showing: `#<number>` as a link/label, review decision (e.g., "Approved", "Changes Requested"), CI status (pass/fail/pending icon), and comment count if non-zero

#### Scenario: Task with linked branch but no PR yet

- **WHEN** the detail view opens for a task with a linked branch but no PR found
- **THEN** the header SHALL display "Branch: <name>" with a note "No PR found"

#### Scenario: Task with no linked branch

- **WHEN** the detail view opens for a task with no `pr_meta`
- **THEN** the header SHALL NOT display any PR section (same as current behavior)

#### Scenario: PR is merged

- **WHEN** the detail view shows a task whose linked PR has state MERGED
- **THEN** the PR section SHALL display "Merged" with appropriate styling

#### Scenario: PR is draft

- **WHEN** the detail view shows a task whose linked PR is a draft
- **THEN** the PR section SHALL display "Draft" indicator alongside other status fields

#### Scenario: Open PR URL

- **WHEN** the user presses `o` while viewing a task with a linked PR
- **THEN** the PR URL SHALL be opened in the default browser using the existing clipboard/browser-open mechanism
