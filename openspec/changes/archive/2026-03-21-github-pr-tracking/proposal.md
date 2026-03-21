## Why

Legato tracks tasks through Backlog → Ready → Doing → Review → Done, but the Review column is a blind spot. When a developer pushes code and opens a PR, there's no visibility into whether it's been approved, whether CI is passing, or whether reviewers have left comments that need addressing. Tasks sit in Review when they actually need rework, and the developer has to context-switch to GitHub to check. By tracking PR state directly on the task, legato can surface actionable signals (failing CI, change requests, approvals) that tell the developer when to act.

## What Changes

- Track a branch name and/or PR number on any task (local or synced)
- New GitHub engine package that queries PR state via the `gh` CLI — no token management, leverages existing auth (same pattern as staccato)
- Batch-fetch PR status for all tracked branches concurrently using `gh pr list --head <branch> --json ...` (one call per branch, fanned out with goroutines)
- On app load and on a configurable interval, fetch status for all tracked PRs: approval state, CI check results, review decision, draft status, merge state
- Surface PR state visually on board cards: CI pass/fail icon, approval badge, comment indicator
- Show PR details in the detail view: approval status, CI status, review decision, PR URL
- CLI subcommand to link a branch/PR to a task: `legato task link <task-id> --branch <branch>`
- Auto-link: when an agent spawns on a task, detect the current git branch and link it

## Capabilities

### New Capabilities
- `github-pr-client`: GitHub PR state fetching via `gh` CLI — query PRs by branch name, return approval state, CI check rollup, review decision, draft/merge status. Concurrent fan-out for batch queries.
- `pr-tracking`: PR-to-task linking, periodic polling service, state aggregation. Stores branch/PR metadata on tasks and refreshes on load + interval.

### Modified Capabilities
- `detail-view`: Add PR status section showing approval state, CI results, review decision, and clickable PR link
- `sqlite-store`: New migration adding PR tracking fields (branch, pr_number, pr_state JSON) to tasks
- `kanban-board`: Render PR state indicators on cards with linked PRs (CI icon, approval badge)
- `legato-cli`: Add `task link` subcommand for manually associating branches with tasks

## Impact

- **New dependency**: Requires `gh` CLI installed and authenticated — graceful degradation if missing (no PR indicators, warning on first attempt)
- **Database**: New migration adding PR metadata columns or extending remote_meta JSON
- **Config**: Optional `github` section in config.yaml for polling interval (default reuses sync interval)
- **Engine layer**: New `internal/engine/github/` package wrapping `gh` CLI calls
- **Service layer**: PR polling lifecycle integrated with existing event bus; enriches CardData for TUI
- **TUI**: Card rendering changes (new indicators), detail view additions (PR section)
- **CLI**: New `task link` subcommand, IPC broadcast for live updates
