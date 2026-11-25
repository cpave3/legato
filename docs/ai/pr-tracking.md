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

A task tracks an ordered list of branches, each with its PR (`task_branches`, migration 082; see [Database](database.md)). The list is kept in the order branches were tracked, and each row holds the PR cache of its branch as `store.PRMeta` JSON: number/URL/state, draft, review decision, checks, comments, `base` (GitHub's `base.ref`, which `prFromStatus` keeps; `samePR` compares it, so the next refresh fills it for a PR tracked before it was kept), pin/match method, and the latest discovery outcome/error.

**Order at read.** `TaskPullRequests` (`pr_branches.go`, `stackOrder`) returns the PRs in the order they merge, bottom first, whatever order they were tracked in: each repository keeps the place of its first PR; within one, the roots (a PR whose base is no other PR's head, such as `main`) come first by number, then the PRs built on each, level by level, each PR's children by number; a PR with no base yet follows in stored position, with those built on it. Merged layers GitHub retargeted to `main` are roots, so they stay at the bottom by number. Every reader of the list goes through it: the board, away tasks, task detail and Attention. Clearing or rediscovering PRs does not discard code context.

`tasks.pr_meta` is now a summary written after the list: the first PR in stack order (with its `repo` and `branch`), or, with none, how discovery last ended (`no_match`, `ambiguous`, `error`). Readers that have not moved to the list use it: reporting's merged-PR count, Argus owner lookup (`owner_lookup.go`, `ListTasksForRepository`), the TUI's targeted-action message, and the `pr_meta` field of task detail JSON (web, CLI), which the web UI still reads for the sync outcome and "Open in browser". Contract it when those readers move.

### Tracked branches

Branches come from (`store.TaskBranch.Source`):

- `code_context`: the code context's branch, unless it is a default branch. A context whose repository or branch changes stops tracking the old branch when this was why it was tracked (`ForgetContextBranch`).
- `stack`: the Staccato stacks `TaskStackService` recorded in `clone_stacks` for the task's job clones, branches `pushed` or `unpushed`, in the stack's depth-first order.
- `api`: `AddTaskBranches` (below), for handoff: the branches a worker reports for an away task (§ Away tasks), and those Return brings back.
- `manual`: the head branch of a PR linked by number (`LinkPR`, pinned).
- `discovered`: found by commit or ticket key when no branch had a PR. Rediscovery drops them.
- `review`: on a worker's card, the stack of open PRs its review loop found on GitHub's base→head chain (`HandoffReviewCaller`); the summary row reports them first, so the main tracks every layer (§ Away tasks).
- `agent`: a branch the task's agent pushed under `legato/` or opened a PR from, seen by the credential broker (§ Agents' branches), or the branch an agent's checkout was on before it moved (`followCheckout`, below). Survives rediscovery and code-context changes.

`store.TrackTaskBranches` merges a list into the task's: branches already tracked keep their PRs and source, and the given ones are placed together, in the given order, where the first of them already was (appended when none was). So a stack that grows or is reordered stays whole and in order, and a code-context branch that is not in the stack stays before it.

The service API for other features (`service.PRBranchActions`, implemented by the PR tracking service):

- `AddTaskBranches(ctx, taskID, repo, branches) (added []string, err)`: add branches of one repository in stack order; returns those not tracked before. No GitHub call. `service.AddTaskBranches(ctx, db, ...)` is the same without the PR service, for when GitHub tracking is off.
- `DiscoverPRs(ctx, taskID)`: find the PRs of branches that have none, one listing per repository, then fetch each found PR by number for its checks and reviews. PRs found before are not refetched. Call it when `AddTaskBranches` added any.
- `DiscoverAndRefreshPRs(ctx, taskID)`: `DiscoverPRs` that also refetches the PRs found before by number, in one pass, returning any GitHub error. A workflow agent step's filled `prs` output uses it at the step's signal, so a PR marked ready, closed or merged since an earlier step is not read stale (see [Workflows](workflows.md) § Agent waits, timeouts and clones).
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

### Agents' branches

The credential broker sees an agent open a PR or push before GitHub's listing does, so the task tracks the branch and looks for its PR at once (LEG-437), as for away tasks. Without this a card, Attention item or review loop started between an agent's `gh pr create` and the next poll (`github.poll_interval_seconds`) saw no PR.

- **Sources.** The proxy's backend, `CredentialChannels`, tells its branch listener (`SetBranchListener(func(service.AgentBranch))`) with the session's task and execution (from the placeholder):
  - A REST `POST /repos/{owner}/{repo}/pulls` through the proxy that GitHub answered 2xx (`credential.BranchListener.BranchOpened`, from a `pr.create` or `pr.create-draft` use whose resource names the head branch; `openedBranch` in `httpproxy.go`). It is told before the response body is relayed, so Legato hears of it before the agent does.
  - gh, which talks to GitHub itself with a token: after a revoke-path gh command that exited 0, the gh wrapper runs `legato credential gh-opened -- <args>`. `cli.GHOpenedPR` knows `gh pr create` (`--head`, else the current branch), `gh api` POSTs to `repos/<o>/<r>/pulls` with a `head` field, and `gh api graphql` `createPullRequest` with a `headRefName` (literal or a variable), reading the body from `--input <file>` (a JSON object, with `variables`) and the query from `query=@<file>` as gh does; `--input -` (gh's stdin, gone by then) is not heard. It posts `{repo, branch}` to `/c/<credential>/pr-opened` (`servePROpened`: placeholder checked, branch validated, no token given out), which calls the same listener. Opened from the person's own login (no Legato token) it is not heard; the poll finds it.
  - A push GitHub accepted (`CredentialChannels.Pushed`): each `refs/heads/legato/…` ref, `Opened` false. The same push goes whole to every push listener (`AddPushListener(func(service.GitPush))`: task, execution, repository, refs): the repository cache's fetch (`RepositoryService.FetchPushed`) and the agent's stage (`WorkStageService.Pushed`: evidence, and Reviewing on a branch whose PR is open; see [Agent states](agent-states.md) § Stages).
- **Tracking** (`service.AgentBranches`, `internal/service/agent_branches.go`, wired by `approvalRuntime.trackAgentBranches` in `cmd/legato/agent_branches_runtime.go`, each event on its own goroutine). It first tracks the task's code-context branch (so the branch the agent started on stays below those it built), then the event's branch with source `agent`. A pushed branch outside `legato/` is ignored; one the agent opened a PR from never is. An ephemeral task's branches are not tracked (it links no PRs). `DiscoverPRs` runs when the branch is new, when it is tracked with no PR yet (so a stack whose PRs opened after a first look is found whole at the next push, and a new branch on a task whose PR merged gets its own), or when a PR was opened from it, through `awayPRs.discover` (one per task at a time, once more when another event came meanwhile). An event for no task (`store.ErrNotFound`) does nothing. Without PR tracking the branches are only recorded.
- **Following the checkout** (`prTrackingService.followCheckout`, at the start of every `syncTask`: polls, `DiscoverPRs`, rediscovery). A code context with `branch_source` `agent` or `job_clone` and a working directory the task uses now (`ownsCheckout`: the task's running main-slot agent works there and no other task's running agent does; who a job clone was made for or attached to does not count, since a clone is reused by another task once its agent ends) follows the branch that directory has checked out now, read by `SetCheckoutHead` (the runtime gives `gitrepo.Head`, which reads git's files: no git runs in the agent's clone). The old branch is tracked as `agent`, the context's branch, head SHA and `branch_source: agent` are rewritten with `store.SetCodeContext` (not `SetTaskCodeContext`, which would forget the old branch and the PR summary). A directory an ended agent left (which another task's agent may now use on its own branch), a `manual` repository or branch, a `worktree` or `handoff` context, a detached HEAD, a missing checkout or a default branch is never followed.
- **Starting an agent again** (`agentService.captureAgentCodeContext`, after each spawn with a working directory: a resume, a reload). A checkout on the code context's repository and branch only looks for the PRs of branches that have none (`DiscoverPRs`), so the stack the task tracks, `discovered` branches included, is kept. A checkout on another branch rediscovers (`RediscoverPR`).
- **Looking again for a repository** (`prTrackingService.DiscoverRepository(ctx, repo)`, `service.ReviewRunDiscover`): `syncTask` in discover mode for every PR sync candidate whose code context or tracked branches are in the repository, with one listing, under the poll lock. The review-run caller calls it when `agent_author` finds no linked task, then looks again before refusing ([Integrations](integrations.md) § Review passes).

### Away tasks

A main tracks the PRs of a task away on a worker itself, with its own read token, as for any task; the worker does no PR tracking for handoff (decision LEG-13). Each summary row the worker sends lists the branches pushed for the task, in stack order (the stack its review loop found on GitHub, then Staccato's stack in the job clone, or the handoff branch; see [Integrations](integrations.md) § Live away state). `WorkerConnections.applyRow` hands them to `awayPRs.track` (`internal/service/away_prs.go`) before it keeps the row:

- The branches are added to the task's tracked branches in the row's order with the away task's repository, source `api` (`PRBranchActions.AddTaskBranches`). No separate `away` source: an away task's branches and those Return adds are the same branches, and `api` survives rediscovery and code-context changes.
- When any branch is new, `DiscoverPRs` runs for the task at once, not at the next poll. It runs in its own goroutine, never on the connection's read loop, so a slow GitHub does not hold up the worker's rows. One discovery runs per task at a time: a branch that arrives while one runs makes it run once more when it ends (`inFlight`/`again`), not a second one beside it. A discovery under way finishes after its connection drops; stopping the connections waits for it.
- After that the PRs follow normal polling. An away task is an ordinary, non-ephemeral task on the main, so `ListPRSyncCandidates` lists it, and its tracked branches bring it into `syncTask` even without a code context.
- Without PR tracking (no GitHub credential) the branches are still recorded (`service.AddTaskBranches`), and nothing is looked for.

The runtime gives the connections the PR service after it starts them (`mainHandoffs.trackAwayPRs`, `WorkerConnections.SetPRTracking`); the handoff test harness passes it as `WorkerConnectionsOptions.PRs` (`MainOptions.GitHub`, a `FakeGitHub`). Rows are applied again at every pull, so a row heard before the PR service was set is tracked at the next connect.

Attention's policy is the same as for any task: drafts raise nothing, a ready PR with a blocker raises a Merge blocker whose **Open task** link (`/board?highlight_task=<id>`) selects the task's row in the web board's Away lane. At Return the main already tracks the PRs: Return's `AddTaskBranches` call with the returned branches adds nothing new and keeps every PR.

The web board's away row sums the PRs up in one line (`awayPRSummary` in `web/src/lib/awayPullRequests.ts`: "PR #12 · draft", "3 PRs · 2 draft · 1 ready · #14 CI failed", red when a ready PR has a blocker), and the away task panel lists each one (`AwayPullRequests`: number, draft or ready, checks, review), then each reported branch without a PR as "no PR yet".

### GitHub client and synchronization

`internal/engine/github` uses the GitHub HTTP API with bearer authentication, API-version headers, bounded request concurrency, and typed authentication/authorization/not-found/rate-limit/server/response errors. Local `git` remains responsible only for inspecting explicit checkout paths.

Periodic synchronization lists all active, non-ephemeral tasks. An ephemeral task (an agent with no card) links no PRs at all (LEG-519): `syncTask` does nothing for one, so neither the discovery an agent's start runs (`captureAgentCodeContext`), rediscovery nor `DiscoverRepository` links its working folder's branch's PR, and `AgentBranches.Track` tracks none of the branches its agent pushes or opens a PR from. Polls never refresh an ephemeral task, so a link would go stale, and the PR of the folder's branch is rarely the agent's work. A review loop with `agent_author` therefore finds no ephemeral author; an explicit `LinkPR` still links. A task with repository context or tracked branches is brought up to date branch by branch (`syncTask`); each repository's recent pull requests are fetched at most once per cycle (`prListings`) and matched in memory:

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

TUI: lowercase `p` links a known PR, `ctrl+r` refreshes the selected task's PR, `ctrl+p` rediscovers it, and lowercase `r` runs the shared provider synchronization used by the WebView. It attempts each configured Jira/GitHub source even if another fails and reports partial failures instead of silently succeeding. Uppercase `T` and `P` open Reporting and Plans; `R` opens the Reeve. Task detail shows repository/branch plus `no match`, `ambiguous`, and API error outcomes.

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

## Reviews and pr.changes_requested

`github.Client.FetchPRByNumber` keeps every review (`PRStatus.Reviews`: id, state, login, user type, submitted time), reading `/pulls/{n}/reviews` a hundred to a page until a short page (`fetchReviews`, at most `maxReviewPages` 30), and folds the decision from them (`reviewDecision`). `refreshBranchPR` passes them to `observeReviews` (`pr_reviews.go`), so the trigger costs no GitHub call of its own. `store.RecordPRReviews` (migration 126, `pr_reviews_seen`: repo, PR number, review id, state, login) records them and returns those not seen before. The first time a PR is recorded it is baselined (a row with review id 0): only reviews submitted after the task began to track the branch (`TaskBranch.AddedAt`) count as new, so what a PR had before a task owned it never fires. Each new `CHANGES_REQUESTED` review is published as `events.EventPRChangesRequested` (`PRChangesRequestedPayload`: task, repo, number, URL, review id, reviewer), unless its login is one of Legato's own Apps: `github.app_logins` (`SetGitHubAppLogins`) or one learned in `github_app_logins` (`RecordGitHubAppLogin`, which `ReviewPublisher.SetLoginRecorder` calls with the login each review is posted as; `learningPublisher` in `cmd/legato/github_runtime.go`). The operator's own reviews count.
