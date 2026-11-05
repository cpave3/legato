## GitHub PR Tracking

Read-only PR enrichment is independent of ticket providers: local and Jira tasks can discover and track pull requests without Staccato. By default Legato reuses an authenticated GitHub CLI session only to obtain an in-memory credential; all repository data is read through the typed HTTP client.

### Configuration

The default `auto` mode prefers an explicit token and otherwise runs `gh auth token` once. Most local users need only:

```bash
gh auth login
gh auth status
```

Configuration:

```yaml
github:
  auth: auto # auto (default), gh, or token
  # token: ${LEGATO_GITHUB_TOKEN} # optional in auto; required in token mode
  poll_interval_seconds: 600
  resolved_poll_interval_seconds: 600

repositories:
  - name: legato
    repo: cpave3/legato
    primary_dir: /home/me/Projects/legato
    base_branch: main
    jira_projects: [LEG]
    workspaces: [Legato]
```

`auto` uses `github.token` when present and never invokes `gh`; otherwise it executes exactly `gh auth token`. `gh` mode requires that CLI credential, while `token` mode requires explicit configuration. The credential stays in memory and is never persisted, serialized, or included in errors. A GitHub CLI OAuth credential commonly has access to private repositories your account can see, subject to organization OAuth restrictions and SAML SSO authorization; verify a repository with `gh repo view OWNER/REPO`.

The underlying OAuth/PAT may have broader scopes, including write access. Legato independently enforces read-only behavior: the GitHub client exposes read operations only and a shared transport rejects POST, PUT, PATCH, DELETE, or any other non-GET request before network dispatch. Legato uses no `gh api`, `gh pr`, or mutation command. Future write support requires a separate explicit design change.

Agents' own GitHub access (reads, pushes and pull requests through a GitHub App, the `github` credential profile) is separate and does not use this client or its credential; see [Credentials](credentials.md) § GitHub.

`repositories` is the shared catalog for workflow inputs, task context discovery, and workspace/Jira mappings. Legacy `workflows.repositories` is loaded when no shared catalog exists, but new configuration should use the top-level key.

### Durable code context

Repository identity is stored separately from fetched PR state in `tasks.code_context`:

- repository (`owner/repo`), branch, head SHA, working directory, and base branch;
- independent repository/branch provenance;
- linked and updated timestamps.

A task tracks an ordered list of branches, each with its PR (`task_branches`, migration 082; see [Database](database.md)). The list is in stack order, bottom first, and each row holds the PR cache of its branch as `store.PRMeta` JSON: number/URL/state, draft, review decision, checks, comments, pin/match method, and the latest discovery outcome/error. Clearing or rediscovering PRs does not discard code context.

`tasks.pr_meta` is now a summary written after the list: the first PR in stack order (with its `repo` and `branch`), or, with none, how discovery last ended (`no_match`, `ambiguous`, `error`). Readers that have not moved to the list use it: reporting's merged-PR count, Argus owner lookup (`owner_lookup.go`, `ListTasksForRepository`), the TUI's targeted-action message, and the `pr_meta` field of task detail JSON (web, CLI), which the web UI still reads for the sync outcome and "Open in browser". Contract it when those readers move.

### Tracked branches

Branches come from (`store.TaskBranch.Source`):

- `code_context`: the code context's branch, unless it is a default branch. A context whose repository or branch changes stops tracking the old branch when this was why it was tracked (`ForgetContextBranch`).
- `stack`: the Staccato stacks `TaskStackService` recorded in `clone_stacks` for the task's job clones, branches `pushed` or `unpushed`, in the stack's depth-first order.
- `api`: `AddTaskBranches` (below), for handoff.
- `manual`: the head branch of a PR linked by number (`LinkPR`, pinned).
- `discovered`: found by commit or ticket key when no branch had a PR. Rediscovery drops them.

`store.TrackTaskBranches` merges a list into the task's: branches already tracked keep their PRs and source, and the given ones are placed together, in the given order, where the first of them already was (appended when none was). So a stack that grows or is reordered stays whole and in order, and a code-context branch that is not in the stack stays before it.

The service API for other features (`service.PRBranchActions`, implemented by the PR tracking service):

- `AddTaskBranches(ctx, taskID, repo, branches) (added []string, err)`: add branches of one repository in stack order; returns those not tracked before. No GitHub call. `service.AddTaskBranches(ctx, db, ...)` is the same without the PR service, for when GitHub tracking is off.
- `DiscoverPRs(ctx, taskID)`: find the PRs of branches that have none, one listing per repository, then fetch each found PR by number for its checks and reviews. PRs found before are not refetched. Call it when `AddTaskBranches` added any.
- `TrackedBranches(ctx, taskID)`: the list, each with its PR.

Readers get a task's PRs through `service.TaskPullRequests(task, branches)`: the PRs of the list in order, else the one PR in `pr_meta` for a task linked before the list existed. `CardDetail.PullRequests` and `CardDetail.Branches` carry them, and `BoardService` implements `PullRequestLister.ListPullRequests(ctx, taskIDs)` for the TUI board, which needs no GitHub credential.

A task linked before migration 082 has its PR only in `pr_meta`. The next refresh of resolved PRs (the startup `PollAll`, or `RefreshPR`) fetches it by number and moves it onto its head branch, keeping its pin and match method. Until then readers show it from `pr_meta` and discovery leaves the task alone, as it did a resolved one.

Context precedence is `manual > worktree > agent directory > repository mapping > inferred ticket-key branch`:

1. Manual context is never overwritten automatically.
2. Yggdrasil creation inspects the returned worktree path. External `legato task worktree set` updates route through the same checkout inspector.
3. Agent launch inspects the exact selected working directory, not Legato's process directory.
4. Exactly one matching Jira-project or workspace repository preset establishes repository context for tasks without a checkout.
5. Once a repository is known, synchronization may infer a branch from one unambiguous task/ticket-key token match.

Tasks without repository context remain fully usable. GitHub discovery skips them and reports missing context rather than scanning arbitrary repositories or matching titles.

### GitHub client and synchronization

`internal/engine/github` uses the GitHub HTTP API with bearer authentication, API-version headers, bounded request concurrency, and typed authentication/authorization/not-found/rate-limit/server/response errors. Local `git` remains responsible only for inspecting explicit checkout paths.

Periodic synchronization lists all active, non-ephemeral tasks. A task with repository context or tracked branches is brought up to date branch by branch (`syncTask`); each repository's recent pull requests are fetched at most once per cycle (`prListings`) and matched in memory:

1. Each tracked branch without a PR takes the PR whose head is that branch in that repository (fork head repositories excluded); of several, an open one, else the newest.
2. Only when no branch has a PR: exact head SHA, then one boundary-aware task/remote-ticket key in a branch. Several key matches that form one stack (each PR's base is the head of another, one bottom, no forks; `stackChain`, using GitHub's `base.ref`) are all tracked in stack order as `discovered`; otherwise the result is `ambiguous`. Zero candidates produce `no_match`.

Default/detached branches and title-only matches never auto-link. API failures record `error` on the branch (and in the summary) while preserving the last-good PR cache.

Found PRs refresh by repository and number on the slower cadence, each branch on its own. A branch's PR is never replaced by discovery once found, so polling never replaces a pinned link. Existing numbered links were pinned by migration 047; branch-only legacy/Staccato links were backfilled as discoverable code context. When discovery links a new PR, the same pass immediately fetches it by number so checks, reviews, and comments are complete before a manual sync returns. Legacy branch-only `pr_meta` without code context still uses the old branch query.

A targeted rediscovery (`RediscoverPR`) works across all of a task's branches: it forgets their PRs, pins and the `discovered` branches, then performs one bounded repository request per repository (`GET /repos/{owner}/{repo}/pulls?state=all&per_page=100`), matches locally, and fetches each PR it links by number. With no match it makes only the listing request. The HTTP client timeout is 30 seconds. TUI completion replaces the progress indicator with the linked PR number, `no match`, ambiguity guidance, or the concrete API/auth/rate-limit error.

### User actions

- **Refresh PR** refetches every linked PR of the task by number without reconsidering identity.
- **Rediscover PR** clears only cached/derived PR state of every branch, retains code context and the branches of the context, stacks and handoff, removes pinning, and immediately reruns matching.
- **Edit context** records a manual repository/branch/SHA/base branch with highest precedence.
- Explicit PR-number linking remains pinned until rediscovery.

CLI:

```bash
legato task github context TASK-123 --repo acme/widget --branch feature/TASK-123
legato task github refresh TASK-123
legato task github rediscover TASK-123
```

`legato task link` remains compatible for hooks and scripts but now writes durable code context. `legato task unlink` clears both the PR cache and code context. Staccato can still call these commands, but is no longer required.

TUI: lowercase `p` links a known PR, `ctrl+r` refreshes the selected task's PR, `ctrl+p` rediscovers it, and lowercase `r` runs the shared provider synchronization used by the WebView. It attempts each configured Jira/GitHub source even if another fails and reports partial failures instead of silently succeeding. Uppercase `R` and `P` retain their existing Reporting and Plans actions. Task detail shows repository/branch plus `no match`, `ambiguous`, and API error outcomes.

Web/API task detail includes `code_context`, the `pr_meta` summary, `pull_requests` (every PR in stack order) and `tracked_branches` (`repo`, `branch`, `source`, optional `pr`). Board cards carry `pull_requests` (omitted when none); the single `pr_number`/`pr_check_status`/`pr_review_decision`/`pr_comment_count`/`pr_is_draft` card fields are gone. The web card shows one line per PR, and the task panel, task detail and related-work panel list them all. The TUI card names each PR with its state when there are several, the task panel has a row per PR, and the detail header has one `PR n/N` entry per PR. Authenticated targeted endpoints are:

- `PUT /api/tasks/{id}/code-context`
- `POST /api/tasks/{id}/refresh-pr`
- `POST /api/tasks/{id}/rediscover-pr`
- existing `POST /api/tasks/{id}/link-pr` and `POST /api/tasks/{id}/unlink-pr`

The web detail panel displays repository context and exposes refresh/rediscover controls. Authenticated `POST /api/board/sync` runs the same single-flight provider synchronization as the TUI and returns `completed`, `failed`, or `unavailable` outcomes for Jira and GitHub; overlapping manual runs return `409 Conflict`. Both standalone `legato serve` and the TUI runtime construct optional GitHub tracking when credentials are available.

### Events and cadence

`PollOnce` uses the unresolved interval and only refreshes resolved PRs when their cadence is due. `PollAll` is used at startup and by global manual refresh; scheduled and forced polls are serialized to avoid duplicate GitHub traffic. Meaningful changes publish `EventPRStatusUpdated`, which the web server maps to `cards_changed`; CLI mutations broadcast `pr_linked` IPC so running TUI/web clients reload.
### Attention dashboard projection

Attention consumes only persisted PR caches (`task_branches`, and `pr_meta` for a task not yet moved onto its branch); `GET /api/attention` never performs a GitHub request. Each PR of a task is judged on its own and raises its own item (source key `<task>:<number>`), so a stack can have several. After a meaningful sync change, the existing `EventPRStatusUpdated` both retains its `cards_changed` board-refresh behavior and triggers an additive PR attention reconciliation.

An active non-draft PR appears under **Merge blockers** when its aggregate review decision is `REVIEW_REQUIRED` or `CHANGES_REQUESTED`, its check status is `fail`, or a combination. Passing/pending/no checks, approval/empty review decisions, drafts, closed/merged PRs, and raw comment totals do not create an item. The projection does not claim complete GitHub mergeability: unresolved review threads, branch protection, conflicts, required checks beyond the persisted aggregate status, and merge-queue state are not currently persisted.
