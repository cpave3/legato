## Context

Legato has a provider-agnostic task model with nullable `provider`/`remote_id`/`remote_meta` fields on the `tasks` table. Jira is the first provider, using a sync service that pulls/pushes task state. The Review column currently has no special behavior — tasks sit there with no feedback from the code review process.

The staccato project (same developer, Go-based) already solves PR status fetching via the `gh` CLI with concurrent per-branch queries using `gh pr list --head <branch> --json ...`. This is a proven pattern we can adopt directly.

GitHub PR tracking differs fundamentally from Jira sync: it's read-only enrichment of existing tasks rather than a bidirectional sync of task state. A task in the Review column may be a local task, a Jira-synced task, or any other provider — the PR link is orthogonal to the ticket provider.

## Goals / Non-Goals

**Goals:**
- Link any task to a git branch, automatically discovering the associated PR
- Periodically fetch PR state (approval, CI, review decision, draft/merge status) for all linked tasks
- Surface PR state on board cards and in the detail view so developers know at a glance if rework is needed
- Provide CLI subcommand for manual branch linking
- Auto-link branches when agent sessions spawn

**Non-Goals:**
- GitHub as a ticket provider (no issue sync, no creating/closing issues)
- Bidirectional sync (not pushing task state to GitHub)
- Review comment content display (just counts/presence — full review reading is staccato's domain)
- PR creation from legato
- Multi-repo support (single repo assumed — the repo the working directory is in)

## Decisions

### 1. Use `gh` CLI, not GitHub API library

**Choice**: Shell out to `gh pr list` and `gh api` via `exec.Command`, same as staccato.

**Why**: No token management, no OAuth flow, leverages existing `gh auth login`. Pure Go binary stays CGO-free. The `gh` CLI handles pagination, auth refresh, and enterprise GitHub instances transparently.

**Alternative considered**: `google/go-github` library — requires token management, adds dependency, and we'd need to handle GitHub Enterprise URL configuration. The `gh` CLI already solves all of this.

### 2. PR tracking is orthogonal to ticket providers

**Choice**: Store PR metadata directly on the task (new `pr_meta` JSON column), not inside `remote_meta`.

**Why**: A Jira-synced task and a local task can both have PRs. Mixing PR data into `remote_meta` couples GitHub state to the ticket provider. A separate `pr_meta` field keeps concerns clean and avoids sync conflicts where a Jira pull overwrites PR data.

**Structure**:
```json
{
  "branch": "feature/foo",
  "pr_number": 123,
  "pr_url": "https://github.com/owner/repo/pull/123",
  "state": "OPEN",
  "is_draft": false,
  "review_decision": "APPROVED",
  "check_status": "pass",
  "comment_count": 3,
  "updated_at": "2026-03-20T10:00:00Z"
}
```

### 3. Concurrent per-branch fetching (staccato pattern)

**Choice**: Fan out one `gh pr list --head <branch>` goroutine per tracked branch, collect results via channel.

**Why**: Staccato proves this scales well for typical developer workloads (5-15 branches). A single GraphQL query would be more efficient but adds complexity for marginal gain at this scale. The `gh` CLI doesn't expose a clean batch-PR-by-branches endpoint.

**Concurrency limit**: Semaphore of 5 to avoid overwhelming the `gh` CLI / GitHub API rate limits.

### 4. Polling model matches sync service

**Choice**: Fetch PR state on app load and on a configurable interval (default: reuse sync interval, or 60s). Publish events via the existing event bus.

**Why**: Consistent with how Jira sync works. The event bus already handles `EventCardsRefreshed` which the TUI subscribes to. No new subscription mechanism needed.

**New events**: `EventPRStatusUpdated` published after each poll cycle.

### 5. Auto-link via git branch detection

**Choice**: When `AgentService.SpawnAgent()` runs, detect the current git branch in the working directory and call the PR linking service. Also detect on manual `task link` CLI command.

**Why**: Reduces friction — developers don't need to manually link every task. The branch name is readily available via `git rev-parse --abbrev-ref HEAD` or from the tmux session's working directory.

### 6. Card indicators — compact PR state badges

**Choice**: Show PR state as small icons/badges on board cards: CI status (checkmark/X/spinner), review decision (approved/changes-requested), and comment count.

**Why**: Cards are narrow — full text doesn't fit. Icons convey state at a glance. Follows the same pattern as agent state indicators and priority borders.

## Risks / Trade-offs

- **`gh` CLI not installed** → Graceful degradation: PR features disabled, warning shown once in status bar. `LookPath` check on startup (same as tmux pattern).
- **Rate limiting** → GitHub API has 5000 req/hr for authenticated users. With 10 tracked PRs and 60s polling, that's ~600 req/hr — well within limits. Semaphore prevents burst.
- **Stale PR data** → 60s polling means up to 60s of stale data. Acceptable for a kanban overview. Manual refresh via sync keybinding (`S`) could also trigger PR refresh.
- **Branch name collisions** → Multiple tasks could track the same branch. First-link-wins, or allow explicit override via CLI. Edge case — most workflows are 1:1.
- **Repo detection** → Assumes legato runs from within a git repo. If not, PR features silently disabled. No multi-repo support in v1.

## Open Questions

- Should PR state influence automatic task movement? (e.g., all checks pass + approved → auto-move to Done). Leaning no for v1 — keep it informational only.
- Should we show a count of unresolved review comments, or just a boolean "has comments"? Depends on what `gh` exposes cheaply.
- Config: should the polling interval be separate from sync interval, or shared? Separate gives more control but adds config surface.
