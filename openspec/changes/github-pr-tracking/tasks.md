## 1. Database Migration

- [ ] 1.1 Create migration `007_pr_meta.sql` adding `pr_meta TEXT` column to `tasks` table (nullable, default NULL)
- [ ] 1.2 Add `UpdatePRMeta(ctx, id, json)` and `ListPRTrackedTasks(ctx)` methods to store with tests (real SQLite in t.TempDir)
- [ ] 1.3 Add `PRMeta` struct to `store/types.go` with JSON serialization and a helper to parse `pr_meta` from task rows

## 2. GitHub PR Client (Engine Layer)

- [ ] 2.1 Create `internal/engine/github/` package with `PRStatus` type and `Client` struct holding a `LookPath` func for testability
- [ ] 2.2 Implement `CheckAvailable()` using injectable `LookPath` — returns error if `gh` not found
- [ ] 2.3 Implement `FetchPRStatus(branch string) (*PRStatus, error)` — shells out to `gh pr list --head <branch>`, parses JSON response
- [ ] 2.4 Implement `deriveCheckStatus()` for CI rollup aggregation (pass/fail/pending/"") with table-driven tests
- [ ] 2.5 Implement `BatchFetchPRStatus(branches []string) (map[string]*PRStatus, error)` — concurrent fan-out with semaphore(5), channel collection
- [ ] 2.6 Implement `FetchCommentCount(owner, repo string, prNumber int) (int, error)` — uses `gh api` to get comment + review_comment totals
- [ ] 2.7 Implement `DetectRepo() (owner, repo string, err error)` — parses `git remote get-url origin` for SSH/HTTPS formats
- [ ] 2.8 Implement `DetectBranch() (string, error)` — runs `git rev-parse --abbrev-ref HEAD`
- [ ] 2.9 Write tests using `exec.Command` injection (mock gh/git output) following the tmux manager test pattern

## 3. PR Tracking Service (Service Layer)

- [ ] 3.1 Define `PRTrackingService` interface in `internal/service/` with `LinkBranch`, `UnlinkBranch`, `PollOnce`, `StartPolling`, `GetPRStatus` methods
- [ ] 3.2 Implement `LinkBranch(ctx, taskID, branch)` — updates store `pr_meta`, triggers immediate `PollOnce` for that branch
- [ ] 3.3 Implement `UnlinkBranch(ctx, taskID)` — clears `pr_meta` to NULL
- [ ] 3.4 Implement `PollOnce(ctx)` — queries `ListPRTrackedTasks`, extracts branches, calls `BatchFetchPRStatus`, enriches with comment counts, updates store, publishes `EventPRStatusUpdated` if changes detected
- [ ] 3.5 Implement `StartPolling(ctx) func()` — goroutine with ticker (configurable interval), calls `PollOnce` each tick, returns stop func
- [ ] 3.6 Implement auto-link hook: function that detects current branch and calls `LinkBranch` if task has no existing `pr_meta`
- [ ] 3.7 Write tests with mock GitHub client and real SQLite (same pattern as sync service with mockProvider)

## 4. CLI Subcommands

- [ ] 4.1 Add `task link <task-id> --branch <branch>` command in `internal/cli/` — loads config+store, calls `UpdatePRMeta`, broadcasts IPC
- [ ] 4.2 Add branch auto-detect when `--branch` flag is omitted — calls `git rev-parse --abbrev-ref HEAD`
- [ ] 4.3 Add `task unlink <task-id>` command — clears `pr_meta`, broadcasts IPC
- [ ] 4.4 Register subcommands in `cmd/legato/` dispatch
- [ ] 4.5 Add IPC message type `pr_linked` for notifying running TUI instances

## 5. Board Card Rendering

- [ ] 5.1 Add `PRState` fields to `CardData` struct in `internal/tui/board/` (CheckStatus, ReviewDecision, CommentCount, IsDraft, PRNumber)
- [ ] 5.2 Populate `CardData.PRState` from task `pr_meta` in app.go data loading (alongside agent state population)
- [ ] 5.3 Add PR indicator icons to `theme/icons.go` — CI pass/fail/pending, approved, changes-requested, comments
- [ ] 5.4 Render PR indicators in `board/card.go` — CI icon, review badge, comment count; skip if no PR linked
- [ ] 5.5 Test card rendering with various PR states (pass/fail/draft/approved/changes-requested)

## 6. Detail View

- [ ] 6.1 Add PR status section to detail view header — PR number, review decision, CI status, comment count
- [ ] 6.2 Show "Branch: <name> — No PR found" when branch is linked but no PR exists
- [ ] 6.3 Add `o` keybinding to open PR URL in browser (reuse existing clipboard/browser-open from `internal/tui/clipboard/`)
- [ ] 6.4 Handle draft PRs with distinct visual treatment in detail header

## 7. App Wiring

- [ ] 7.1 Add optional `github` config section to `config/` — polling interval field (default 60s)
- [ ] 7.2 In `main.go`: detect `gh` availability, create GitHub client, create PRTrackingService, pass to TUI app
- [ ] 7.3 Run initial PR poll on startup (after data load), start polling scheduler alongside sync scheduler
- [ ] 7.4 Handle `EventPRStatusUpdated` in TUI app — trigger board data refresh
- [ ] 7.5 Wire auto-link into `AgentService.SpawnAgent` — call `PRTrackingService.LinkBranch` with detected branch
- [ ] 7.6 Handle `pr_linked` IPC message in TUI — trigger PR poll and board refresh
