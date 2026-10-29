Every task is test-first (red, green, refactor).

## 1. Store and service

- [x] 1.1 Migration 069 `approval_requests`; store tests for insert-or-get on `(source, source_key)`, the conditional decide, list by status and task, and expire by task
- [x] 1.2 `ApprovalService.Submit`, `Get`, `List`; tests that a second submit reports nothing created and that `EventApprovalChanged` is published once
- [x] 1.3 `Decide` with registered resolvers; tests for first-wins, unknown option, resolver failure and the decided record
- [x] 1.4 `Dismiss`, `Withdraw`, `ExpireTask`; tests for each status change and that a closed approval cannot be decided

## 2. Agent request watcher

- [x] 2.1 `ApprovalWatcher` follows streams of running native agents (fake endpoint); test that a `permission_request` becomes a pending approval with its options and a deny option
- [x] 2.2 Snapshot on open; test that a pending permission and a pending question in the snapshot become approvals
- [x] 2.3 Closing events; tests for `permission_resolved`, `question_timeout`, `run_finished`, a snapshot without the request, and `EventAgentDied`
- [x] 2.4 Agent resolver; tests that allow, deny with feedback, answers and dismissal reach `ResolvePermission` and `AnswerQuestion` correctly
- [x] 2.5 Event listeners; the server's context watcher reads `usage_updated` from the watcher; context watcher tests keep passing without their own stream

## 3. Surfaces

- [x] 3.1 Attention producer `approvals` with signal kind `approval`; tests for the items, the actions and executing one
- [x] 3.2 Server approvals API and `approvals_changed`; handler tests for list, get, decide (including 409), dismiss
- [x] 3.3 IPC `approval_changed` in both runtimes; test the TUI and serve handlers publish `EventApprovalChanged`
- [x] 3.4 ntfy click links and `notifications.approvals`; notifier test that the `Click` header is set and one notification per created approval
- [x] 3.5 Web `ApprovalTray` in `Layout`, API client, query sync; component tests for showing, deciding, `?approval=` and an empty list
- [x] 3.6 TUI count line and approval overlay; model tests for `y`, `n`, `j`/`k` and closing when the list empties
- [x] 3.7 Android: `legato://approval` deep link; JVM test for parsing and choosing the server
- [x] 3.8 Wire both runtimes (`legato serve`, TUI with and without the web server)
- [x] 3.9 Docs: `docs/ai/approvals.md`, links from `docs/ai/attention.md`, `web-ui.md`, `tui.md`, `config.md`, `packages.md`; CLAUDE.md doc list
- [x] 3.10 Check in a scratch Legato with a Claude Code agent: a permission appears in the tray, Attention and TUI; approving in one place withdraws it everywhere
