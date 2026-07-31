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

`repositories` is the shared catalog for workflow inputs, task context discovery, and workspace/Jira mappings. Legacy `workflows.repositories` is loaded when no shared catalog exists, but new configuration should use the top-level key.

### Durable code context

Repository identity is stored separately from fetched PR state in `tasks.code_context`:

- repository (`owner/repo`), branch, head SHA, working directory, and base branch;
- independent repository/branch provenance;
- linked and updated timestamps.

`tasks.pr_meta` is a remote cache: PR number/URL/state, reviews, checks, comments, pin/match method, and the latest discovery outcome/error. Clearing or rediscovering a PR does not discard code context.

Context precedence is `manual > worktree > agent directory > repository mapping > inferred ticket-key branch`:

1. Manual context is never overwritten automatically.
2. Yggdrasil creation inspects the returned worktree path. External `legato task worktree set` updates route through the same checkout inspector.
3. Agent launch inspects the exact selected working directory, not Legato's process directory.
4. Exactly one matching Jira-project or workspace repository preset establishes repository context for tasks without a checkout.
5. Once a repository is known, synchronization may infer a branch from one unambiguous task/ticket-key token match.

Tasks without repository context remain fully usable. GitHub discovery skips them and reports missing context rather than scanning arbitrary repositories or matching titles.

### GitHub client and synchronization

`internal/engine/github` uses the GitHub HTTP API with bearer authentication, API-version headers, bounded request concurrency, and typed authentication/authorization/not-found/rate-limit/server/response errors. Local `git` remains responsible only for inspecting explicit checkout paths.

Periodic synchronization lists all active, non-ephemeral tasks and groups unresolved discovery by repository. Each repository's recent pull requests are fetched once per cycle, then indexed in memory. Matching is conservative:

1. exact head SHA;
2. exact repository and branch;
3. one boundary-aware task/remote-ticket key in a branch.

Default/detached branches and title-only matches never auto-link. Fork repository identity participates in exact branch matching. Duplicate ticket-key candidates produce `ambiguous`; zero candidates produce `no_match`. API failures produce `error` while preserving the last-good PR cache.

Resolved and manually pinned PRs refresh by repository and number on the slower cadence. Polling never replaces a pinned link. Existing numbered links are pinned by migration; branch-only legacy/Staccato links are backfilled as discoverable code context.

### User actions

- **Refresh PR** refetches the currently linked PR by number without reconsidering identity.
- **Rediscover PR** clears only cached/derived PR state, retains code context, removes pinning, and immediately reruns matching.
- **Edit context** records a manual repository/branch/SHA/base branch with highest precedence.
- Explicit PR-number linking remains pinned until rediscovery.

CLI:

```bash
legato task github context TASK-123 --repo acme/widget --branch feature/TASK-123
legato task github refresh TASK-123
legato task github rediscover TASK-123
```

`legato task link` remains compatible for hooks and scripts but now writes durable code context. `legato task unlink` clears both the PR cache and code context. Staccato can still call these commands, but is no longer required.

TUI: lowercase `p` links a known PR, `ctrl+r` refreshes the selected task's PR, `ctrl+p` rediscovers it, and lowercase `r` performs the existing global Jira + GitHub synchronization. Uppercase `R` and `P` retain their existing Reporting and Plans actions. Task detail shows repository/branch plus `no match`, `ambiguous`, and API error outcomes.

Web/API task detail includes `code_context` and enriched `pr_meta`. Authenticated targeted endpoints are:

- `PUT /api/tasks/{id}/code-context`
- `POST /api/tasks/{id}/refresh-pr`
- `POST /api/tasks/{id}/rediscover-pr`
- existing `POST /api/tasks/{id}/link-pr` and `POST /api/tasks/{id}/unlink-pr`

The web detail panel displays repository context and exposes refresh/rediscover controls.

### Events and cadence

`PollOnce` uses the unresolved interval and only refreshes resolved PRs when their cadence is due. `PollAll` is used at startup and by global manual refresh. Meaningful changes publish `EventPRStatusUpdated`; CLI mutations broadcast `pr_linked` IPC so running TUI/web clients reload.