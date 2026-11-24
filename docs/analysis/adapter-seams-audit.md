# Adapter Seams Audit: Staccato, Yggdrasil and Burnlite

Audited at `main` @ `6a720102` (2026-11-24). Every file and line below is
from that commit; the [appendix](#appendix-greps-that-reproduce-the-audit)
has the greps that find them again.

This is the read-only audit LGPOL-2 asks for, part of milestone LGPOL-1
("Adapters and seams"). It lists every place where one of three backends
reaches past the place it should be hidden, so that its backend cannot be
swapped by configuration:

- **Stacks:** Staccato (`st`).
- **Worktrees:** Yggdrasil (`yg`).
- **Tickets:** Burnlite.

It builds on two LGPOL-1 Notes and does not repeat them: the architecture
seams review from legato-1 (made at v0.25.0, `1921c975`) and the Reeve's
assessment of 2026-11-24. That review barely covered Staccato and the text
agents read; this audit adds those, the UI copy and packaging. Re-checked on
current main:

- **LEG-400** ("Extensible integration surfaces") has made no progress. It
  holds only its open Meta task, LEG-401.
- `worktree.NewRunner("yg")` is still called 4 times (Y2).
- The `*jiraSource` cast has moved to `internal/service/sync.go:107`. Card
  moves use it through `s.jira()` at `sync.go:364`, `:402` and `:464` (B3).

## Summary

| Backend | Interface today | How it leaks | Blocks swapping? |
| --- | --- | --- | --- |
| **Yggdrasil** (worktrees) | Half of one. `service.worktreeRunner` is private, with two methods (`internal/service/worktree.go:15-18`). Consumers see `server.WorktreeService` and `tui.WorktreeService`. | Its one implementation, `engine/worktree.Runner`, runs `yg`. It is built with `NewRunner("yg")` at 4 sites. The setting is named after the backend (`worktrees.yggdrasil.enabled`). Errors, web UI copy and step descriptions say "yg" or "Yggdrasil". There is no `List`. | **Yes**, but the seam is close to the right shape: the cheapest of the three. |
| **Staccato** (stacks) | **None.** Services call `engine/stackgraph` directly. It reads Staccato's own files (`.git/stack/graph.json`, `refs/staccato/*`) and writes Staccato's `st graph import` document. | The Stacks tab, handoff pull and Return, PR branch order and review-pass guidance all depend on `stackgraph`. Agents are told to use `st`, and only when `st` is on `PATH`. The web UI tells the user to type `st graph import`. | **Yes.** Without `st`, agents get no stacking guidance, and the Stacks tab, stack handoff and take-home do nothing. |
| **Burnlite** (tickets) | **Reads go through `TicketSource`** (`internal/service/ticket_source.go:19-40`). Burnlite plugged in with no sync changes. **There are no write verbs.** | Closing a ticket bypasses the seam: `burnlite.task.close` has its own executor and client. The shipped `ticket-to-merged-pr` workflow uses it. `provider == "burnlite"` is special-cased in the board, TUI and web UI. Card moves are Jira-only, through a cast. Agents reach Burnlite through its own MCP tools (`mcp__burnlite__*`), and the managed skills and the "Ticket to PR" macro name those tools. | **Partly.** Pulling and importing are generic. Closing, claiming, noting and the text agents read are not. |

Patterns to keep and copy:

- `TicketSource` for reads.
- The **tracker doc** that the planning skills use (`wayfinder`, `to-spec`,
  `to-tickets`). They say "the project issue tracker" and read
  `docs/agents/issue-tracker.md`, so they never hard-code Burnlite.
- `engine/clonesetup` already reimplements Yggdrasil's provisioning and
  `post_create` hooks for job clones (`internal/engine/clonesetup/`). That
  makes a plain-git worktree backend small.
- `FindReviewStack` (`internal/service/review_pass_stack.go:46-55`) finds a
  stack from GitHub's base→head chain. It uses Staccato's graph only for
  ordering, so review passes already work without `st`.
- `LEGATO_HOLDER` (`internal/service/agent.go:979`) is a generic holder, not
  a Burnlite one.
- The handoff brief names the provider through `TicketProviderLabel`
  (`internal/service/task_handoff_types.go:95`).
- The built-in macros (`internal/service/macros.go:40-68`: Plan, Implement,
  Review, Address feedback) name no backend.
- No Reeve prompt (`internal/service/reeve*.go`) and no role prompt
  (`internal/engine/hooks/prompts/`) names `st`, `yg` or Burnlite.

## Leak tables

Kinds: **code**, **workflow**, **agent text**, **UI**, **docs**,
**install**, **setting**.

Severities:

- **blocks**: the backend cannot be swapped by configuration, or a generic
  feature works only with it.
- **cosmetic**: the backend's name shows, and nothing else breaks.
- **legit**: a provider-named page or setting that is right as it is, as
  Jira's are.

### Staccato (`st`)

| # | Where | What | Kind | Severity |
| --- | --- | --- | --- | --- |
| S1 | `internal/engine/stackgraph/stackgraph.go` (whole package; `:62`, `:91`, `:106`, `:224`, `:267-271`) | Reads `.git/stack/graph.json` and refuses a clone with `refs/staccato/*`. `Stack.Document` is Staccato's format-1 `st graph import` document. Errors name `st` ("st graph share", "not JSON st wrote"). | code | blocks |
| S2 | `internal/service/task_stacks.go:16`, `:35-65`, `:86-87`, `:143`, `:206-213` | `TaskStackService` calls `stackgraph.Read` directly; there is no interface. The "importable" document is Staccato's. `internal/server/task_stacks.go:14`, `:48` serve it. | code | blocks |
| S3 | `internal/service/task_handoff_pull.go:15`, `:152-215` | A worker's pull orders the pushed branches with `stackgraph.Read` and `OnRemote`. | code | blocks (falls back to no order) |
| S4 | `internal/service/task_handoff_return.go:35-36`, `:58`, `:154-172` | A handoff Return carries `Stack`, Staccato's import document, between instances: a wire format. | code | blocks |
| S5 | `internal/service/pr_branches.go:20`, `:243` | `BranchSourceStack` means "a Staccato stack recorded for one of the task's job clones". | code | blocks |
| S6 | `internal/engine/store/clone_stack.go:10` | The `clone_stacks` table stores what Staccato keeps. The schema itself is generic (root, branches, problem). | code | cosmetic |
| S7 | `internal/cli/stack_guidance.go:19-29` (`StackSupport`), `:35-50` (`stackGuidanceFor`), `:52-56` (`stackGuidance`) | Every agent is told "Stacks of branches with Staccato (st) … Push the stack with st sync, not git push … st pr", but **only if** `st graph import --help` works. Claude Code reads it in the MCP server's instructions (`internal/cli/mcp.go:139-140`). Codex and Chimera read it in the launch prompt (`cmd/legato/configured_adapters.go:155-167`, probed once in `cmd/legato/stack_support.go`). | agent text | blocks |
| S8 | `internal/cli/mcp_credential_guidance.go:150-158` (`branchRequest`) | With stacks on, it asks for a branch-pattern push grant. The text names no tool. | agent text | cosmetic |
| S9 | `internal/service/review_pass_feedback.go:81-83` (`StackFeedbackSkill`), `:86-104` (`stackHow`), `:160-172` | Tells the author agent `st restack` when the checkout has a Staccato graph, else `git rebase --update-refs`. Names the operator's skill `address-stack-feedback`, which is Staccato-based. Wired through `cmd/legato/review_pass_contract.go:42-44`. | agent text | blocks (a plain-git fallback exists) |
| S10 | `web/src/components/board/TaskStacks.tsx:67` | Shows `st graph import ~/Downloads/<name>` with a copy button: the user must run `st`. | UI | blocks |
| S11 | `web/src/components/board/TaskStacks.tsx:14-15`, `web/src/lib/task-stacks.ts:3`, `:35` | Comments say "built with Staccato". | UI | cosmetic |
| S12 | `internal/service/update_tools.go:70-73` (`NewSTUpdater`); the generic copy in `internal/service/update_attention.go:53-57` and `web/src/components/UpdatesSettings.tsx:132`, `:137` | `st` self-update. The Attention item and Settings show "Update st". | install / UI | cosmetic |
| S13 | `internal/service/doctor.go:215-216`; printed by `internal/setup/flow.go:202-205` | Doctor check "Staccato (st)" with "Install Staccato: https://github.com/cpave3/staccato". Optional; setup lists it when it is missing. | install | cosmetic |
| S14 | `internal/engine/hooks/staccato.go` (`legato hooks install --tool staccato`); `cmd/legato/command_tree.go:340`, `:343` | An `st` `post-pr-create` hook that calls `legato task link`. PR tracking already finds PRs by branch, so the hook repeats the generic path. | code | cosmetic |
| S15 | Site: `integrations/staccato.md` (15 mentions), `running/agent-server.md:47` (the `curl … st-linux-amd64` install), `running/updates.md`, `using/web-ui.md`, `integrations/github.md`, `using/handoff.md`, `using/review-passes.md`, `reference/cli.md`, `security/credentials-setup.md`, `contributing/architecture.md`. `README.md:18`. | Users are taught `st` commands. | docs | blocks (the docs teach the backend) |

There is **no setting**: stacks are on exactly when `st` is on `PATH`.

### Yggdrasil (`yg`)

| # | Where | What | Kind | Severity |
| --- | --- | --- | --- | --- |
| Y1 | `internal/engine/worktree/runner.go:12-53` | The only runner. It runs `yg new <branch> --agent-owned --print-path --base <base>` and `yg remove <branch> [--force]`. Errors say "creating worktree with yg", "yggdrasil produced no path", "removing worktree with yg". | code | blocks |
| Y2 | `cmd/legato/main.go:1034-1035`, `:1441-1442`; `cmd/legato/workflow_runtime.go:142-143`; `cmd/legato/integration_runtime.go:110-111` | `worktree.NewRunner("yg")` hard-coded 4 times behind `cfg.Worktrees.Yggdrasil.Enabled`. It gates task worktrees, `github.pr.prepare`, `worktree.remove` and Argus delegation worktrees. | code | blocks |
| Y3 | `internal/service/worktree.go:15-18`, `:84` | `worktreeRunner` is private and has no `List`. One error says "yggdrasil returned an invalid path". | code | blocks |
| Y4 | `config/config.go:157-165`, `config/settings.go:192` | The setting `worktrees.yggdrasil.enabled` ("Make worktrees with Yggdrasil … with yg"). The user turns on a generic feature by naming the backend. | setting | blocks |
| Y5 | `internal/engine/workflow/catalog.go:446`, `:516`; `web/src/generated/workflow.schema.json:1546`, `:2470`, `:2715`, `:2751` | Step descriptions "Needs Yggdrasil worktrees." and "Remove the workflow task's Yggdrasil worktree". The step types themselves are generic (`github.pr.prepare`, `worktree.remove`). | workflow | cosmetic |
| Y6 | `web/src/components/board/WorktreeModal.tsx:62` | "Create a Yggdrasil worktree and associate it with {taskId}." | UI | cosmetic |
| Y7 | `web/src/components/ManualWorkflowStepAction.tsx:17` | "Yggdrasil will remove this worktree …" | UI | cosmetic |
| Y8 | `internal/tui/app.go:1601` | Prints "worktree failed: " and Y1's errors, so `yg` shows in the status bar. | UI | cosmetic |
| Y9 | `internal/engine/hooks/yggdrasil.go` (`legato hooks install --tool yggdrasil`) | Writes a `post_create` hook into a repository's `.yggdrasil.toml` that calls `legato task worktree set`. A second backend reports its path directly and needs no hook. | code | cosmetic |
| Y10 | `internal/engine/clonesetup/load.go:24`, `:53`, `:64`, `:87`; `cmd/legato/command_tree.go:180-181`; `cmd/legato/clone_setup.go:30`; `web/src/lib/preparingAgents.ts:15` | `.yggdrasil.toml` is the fallback setup file for job clones, with `YG_*` variables. Read for compatibility, never required. | code / docs | cosmetic (keep it) |
| Y11 | `internal/service/doctor.go:217-218`, `cmd/legato/doctor_cmd.go:52-56` | Doctor check "Yggdrasil (yg)". Required only when a workflow has `worktree.remove`. **Gap:** `github.pr.prepare` and `worktrees.yggdrasil.enabled: true` do not make it required. | install | cosmetic (a doctor bug) |
| Y12 | Site: `integrations/yggdrasil.md` (12 mentions), `using/workflows.md` (4), `reference/cli.md` (3), `reference/keyboard.md`, `reference/configuration.md`, `integrations/github.md`, `integrations/argus.md`, `running/agent-server.md`. `README.md:18`. | — | docs | cosmetic |
| Y13 | `web/src/lib/settingsCatalog.ts:35` | The settings search term "yggdrasil". | UI | cosmetic |

**No agent-facing text names `yg`.** No launch prompt, role prompt, MCP
guidance, Reeve prompt, built-in macro or built-in workflow prompt does. The
Reeve meets Y4 and Y5 only as settings and catalog text.

### Burnlite

| # | Where | What | Kind | Severity |
| --- | --- | --- | --- | --- |
| B1 | `internal/service/workflow_burnlite_close.go` (whole file); `internal/engine/workflow/catalog.go:481-493`; `cmd/legato/workflow_runtime.go:137` | The `burnlite.task.close` step has its own `BurnliteTaskCloser` and `openBurnlite` client. It bypasses `TicketSource`; there is no generic close. | workflow / code | blocks |
| B2 | `internal/engine/workflow/examples/ticket-to-merged-pr.workflow.yaml:9-10`, `:61-68` | The shipped workflow closes "the task's Burnlite ticket". Other providers are skipped (`optional: true`). | workflow | blocks |
| B3 | `internal/service/ticket_source.go:19-40`; `internal/service/sync.go:105-111`, `:364`, `:402`, `:464`; `ticket_source.go:199` | `TicketSource` has no Close, Claim, Note or Move. Card moves go through a `*jiraSource` cast; so does the attachment downloader. | code | blocks (writes) |
| B4 | `internal/service/board.go:143-147` | `if provider == "burnlite"` fills `BlockedBy`, `Milestone` and `Labels` from `remote_meta`. | code | cosmetic (Burnlite-only fields) |
| B5 | `internal/tui/detail/model.go:813-816`; `internal/tui/app.go:2157`; `internal/tui/theme/icons.go:46`, `:73`, `:106`, `:122`; `web/src/lib/providers.ts:6`; `web/src/components/board/DetailPanel.tsx:45-49`; `web/src/lib/boardCard.ts:208-219` | Hand-kept provider labels, icons and Burnlite field names ("Milestone", "Claimed by"). This is LEG-401's `ProviderInfo` finding. | UI | cosmetic |
| B6 | `internal/service/task_handoff_brief.go:92-94`; goldens `internal/service/testdata/handoff_brief/*-burnlite.md`, `*-jira.md` | "you may claim this ticket, add notes to it and close it, as holder … $LEGATO_HOLDER". The verbs are Burnlite's but are offered for Jira tickets too. The provider name itself is generic. | agent text | cosmetic (wrong verbs for Jira) |
| B7 | `internal/service/mcp_connections.go:58`, `:127`, `:696-697`; `docs/ai/integrations.md` § One Burnlite connection (LEG-544) | The board's Burnlite connection is shared with agents as an upstream MCP server, so agents call `mcp__burnlite__claim_task`, `close_task`, `list_tasks` and `get_tracker_doc` by name. There is no Legato-level ticket verb for agents. | agent text | blocks |
| B8 | Managed skills, **operator-owned**, in the Legato skills folder (not in the repository). `implement-stack/SKILL.md:3`, `:6`, `:8`, `:12`, `:14`, `:35-36`, `:47`, `:51`, `:64`, `:82`; `implement-stack/agents/openai.yaml:3`; `proposal-stack/SKILL.md:3`, `:10`, `:66` | "Implement a Burnlite spec … as a local staccato stack". `list_tasks` with `frontier`, `claim_task`, `close_task`, `st append`, `st restack`, `st log`. **These also leak Staccato.** | agent text | blocks |
| B9a | The operator's macro **"Ticket to PR"**, kept in Legato's database (read as this agent's own brief) | "For a Burnlite task, also read it in full with get_task … and claim it (holder: the task key)" and "close it yourself … using close_task". It names Burnlite's tools and no `st`. | agent text | blocks |
| B9b | The operator's macro **"Address Stack Feedback"** (applies to a task; text from the Reeve) | "Please use the address-stack-feedback skill to handle the feedback on this stack of pull requests". The macro names no tool, but the skill is built on Staccato (`st restack`, `st continue`): **a Staccato leak through the skill**, the same skill S9 names. | agent text | blocks (Staccato) |
| B9c | The operator's macro **"Rebase and repush"** (no `applies_to`; text from the Reeve) | Rebases the PR branch onto the latest default branch with the resolve-merge-conflicts skill, runs the checks, `git push --force-with-lease`, waits for CI. Plain git, no `st`, no tracker. | agent text | none |
| B9d | The operator's macro **"Fix CI"** (no `applies_to`; text from the Reeve) | `gh pr view`, `gh run view --log-failed`, reproduce, rerun a failure that also happens on main as flaky, fix, push. Plain git and `gh`: GitHub-only, which is the forge seam (out of scope), not one of these three. | agent text | none (forge only) |
| B10 | `internal/cli/mcp_operator.go:17`; `internal/server/api_reference.go:33`, `:63` | Example keys `LEG-374`, `LEG-12` in a tool schema and the API reference. | agent text / docs | cosmetic |
| B11 | `internal/service/attention_integrations.go:93-95`; `workflow_burnlite_close.go:82`, `:86`, `:90`; `config/settings.go`, `config/config.go` (the `burnlite:` block); `cmd/legato/command_tree.go:237-255`; `internal/setup/flow.go:327`, `:338`; `web/src/components/BurnliteSettings.tsx`, `IntegrationsSettings.tsx`, `MCPSettings.tsx`, `ImportRemoteModal.tsx`; site `integrations/burnlite.md` and others | Provider-named settings, pages, errors and docs. | UI / setting / docs | **legit** (as Jira's are) |

### Across the three

- `README.md:18` lists "Staccato, Yggdrasil, Argus, ntfy and Manticore
  integrations" as features. Cosmetic.
- `st skill` (Staccato's own CLI) installs a Staccato skill for agents. It is
  outside Legato, but it is a second route by which `st` reaches agent text.
- The operator's personal Claude Code skills `address-stack-feedback` and
  `create-stack-prs` are Staccato-based. Legato's code names the first (S9).

## Interfaces: what exists and what is missing

| Seam | Exists | Missing |
| --- | --- | --- |
| Worktrees | `service.worktreeRunner` {Create, Remove}, private. `WorktreeWorkflow` keeps the task's metadata. Consumer interfaces in `server` and `tui`. | An exported `WorktreeBackend` with `List` and `Name`. A factory chosen by a `worktrees.tool` setting (LEG-401 refactor 7). Errors in Legato's words. |
| Stacks | Nothing. `stackgraph` is a Staccato reader in `engine`. | A `StackBackend` in `service`: Read(dir) giving a neutral `Stack`, Append, Restack, Sync (with failed pushes), Log, Import(doc), Continue and Abort. A **Legato-owned stack document** for handoff and take-home, which the Staccato backend converts to and from `st graph import`. A `stacks.tool` setting. |
| Tickets | `TicketSource` (reads); `TicketProviderLister` and `TicketProviderInfo` (`internal/service/ticket_source.go:167-177`). | Optional write capabilities on a source: `TicketCloser`, `TicketClaimer` (holder), `TicketNoter`, `CardMover` (replacing the `jira()` cast; LEG-401 refactor 3) and `TicketLister` (LEG-488's `TicketSource.List`). A `ProviderInfo` that carries field labels and the icon (B5). Legato-level ticket tools for agents. |

## Proposed Legato-level verbs

Agents call these through the host broker, as other `legato` commands
already do. Each one prints `--json`.

### `legato stack …`

| Verb | Staccato | Plain git (`stacks.tool: git`) |
| --- | --- | --- |
| `append <name>` | `st append <name>` | `git switch -c <name>`, then record the parent: `git config branch.<name>.legatoParent <current>` |
| `restack` | `st restack` | From the top branch, `git rebase --update-refs <the root's parent>` (git 2.38 or later). A conflict stops it. |
| `continue` / `abort` | `st continue` / `st abort` | `git rebase --continue` / `--abort` |
| `sync` | `st sync --json` (`failed_pushes`) | `git fetch`; drop merged branches (PR state from PR tracking); restack; `git push --force-with-lease origin <b>` for each branch. Report `failed_pushes` with git's reason: the same JSON contract, so the approval flow ("pending ap-…") does not change. |
| `log` | `st log` | Walk `legatoParent` from the current branch and print the tree. |
| `pr [--draft]` | `st pr make` (cannot open a draft) | `gh pr create --draft --head <b> --base <parent>`, bottom up. This is what the handoff guidance already says (`internal/cli/stack_guidance.go:47`). |
| `import <file>` | `st graph import <file>` | `git fetch` the branches, then write `legatoParent`. |

### `legato worktree …`

| Verb | Yggdrasil | Plain git (`worktrees.tool: git`) |
| --- | --- | --- |
| `add <branch> [--base B]` | `yg new <branch> --agent-owned --print-path --base B` (`LEGATO_TASK_ID` in the environment) | `git worktree add -b <branch> <path> <B>`, then `clonesetup.Load`, `Provision` and `Run` (`.legato.toml`, else `.yggdrasil.toml`), then record it on the task as `legato task worktree set` does. Path: see open question 3. |
| `remove <branch> [--force] [--delete-branch]` | `yg remove <branch> [--force] [--delete-branch]` (by default `yg` deletes the branch only when it is merged) | `git worktree remove [--force] <path>`, then `git branch -d` only when merged (Yggdrasil's rule), `-D` with `--delete-branch` |
| `list` | `yg list` | `git worktree list --porcelain` |

### Ticket verbs

**Workflow steps:**

- `ticket.close {task, reason, summary, optional}` replaces
  `burnlite.task.close`. The old name stays as a deprecated alias for the
  same executor.
- `ticket.claim` and `ticket.release`.
- `ticket.note`.
- `ticket.import` and `tickets.list`, as LEG-488 defines them. LEG-488 stays
  in Ship v1; these reuse its `TicketSource.List`.

Each step fails with "<Provider> cannot close tickets" when its source lacks
the capability, and is skipped when the step is `optional`.

**Agent tools:** `ticket_get`, `tickets_list`, `ticket_claim`, `ticket_note`
and `ticket_close` in `legato mcp serve`. They act on the task's own ticket
unless told otherwise, as holder `LEGATO_HOLDER`, else the task ID, through
`TicketSource` and the board's connection.

| Verb | Burnlite | Jira | Local board (the trivial second backend) |
| --- | --- | --- | --- |
| close | `close_task` | transition to the done status | move the card to Done |
| claim | `claim_task` (holder) | assign to self, or nothing | set `claimed_by` metadata |
| note | `create_note` | add a comment | `AddTaskNote` |
| list (frontier) | `list_tasks` with `frontier` | JQL | the board's tasks |

The **tracker doc** stays the route for planning operations: maps, specs and
a tracker's own frontier rules.

## The smallest plain-git second backend for each seam

1. **Worktrees.** `git worktree add` with `clonesetup`: about 100 lines and
   their tests, because provisioning and hooks already exist. It proves the
   setting, the factory, `github.pr.prepare`, `worktree.remove` and the web
   modal.
2. **Stacks.** Parents in `git config branch.<b>.legatoParent`, restacking
   with `git rebase --update-refs`, pushing with `--force-with-lease` per
   branch and the same `failed_pushes` JSON. The Stacks tab and handoff read
   the stack from the config. It proves the verbs, the reader and the Legato
   stack document.
3. **Tickets.** There is no git tracker. The smallest proof of the *verb*
   seam is the **local board**, which has no outside dependency. The real
   second provider is GitHub Issues: reads through the existing GET-only
   client, writes through a host-side App token as `ReviewPublisher` does,
   claim as assignee. That is a separate, larger ticket.

## Proposed tickets for LGPOL-1

In dependency order. Sizes: S = 1, M = 3, L = 8, XL = 13. These are proposed
only; none is filed.

| # | Ticket | Size | Uncertainty | Blocked by |
| --- | --- | --- | --- | --- |
| 1 | **Worktree backend seam.** Export `WorktreeBackend` {Name, Create, Remove, List}; a factory from a new `worktrees.tool` (`yggdrasil` \| `git`) and `worktrees.enabled`; `worktrees.yggdrasil.enabled` kept as a pinned alias for both; one constructor replaces the 4 `NewRunner("yg")` calls; errors in Legato's words (Y1–Y4, Y8). Supersedes LEG-401 refactor 7. | S | Low | — |
| 2 | **Plain-git worktree backend**, and doctor requirements by tool. Also fixes the Y11 gap: `github.pr.prepare` and the enabled setting require the tool. | M | Medium | 1 |
| 3 | **`legato worktree add\|remove\|list`** as broker ops, so sandboxed agents can call them. | M | Medium | 1 |
| 4 | **Stack backend seam.** `StackBackend` in `service`; the Staccato implementation wraps `stackgraph`; `task_stacks.go`, `task_handoff_pull.go`, `task_handoff_return.go`, `pr_branches.go` and `review_pass_feedback.go` go through it; a `stacks.tool` setting. | M | Medium | — |
| 5 | **Legato stack document v1** for handoff Return and take-home. A handoff feature flag negotiates it; Staccato's backend converts to and from `st graph import`. | M | Medium | 4 |
| 6 | **`legato stack append\|restack\|sync\|log\|pr\|import\|continue\|abort`** over the backend, Staccato first, keeping `st sync`'s `failed_pushes` contract. | L | Medium | 4 |
| 7 | **Plain-git stack backend.** | L | High | 4, 6 |
| 8 | **Agent text and UI to Legato verbs.** `stack_guidance.go` gated on "a stack backend is available", not on `st`; `stackHow` says `legato stack restack`; `TaskStacks.tsx` shows `legato stack import`; the WorktreeModal and ManualWorkflowStepAction copy; catalog descriptions and the regenerated schema; the README line. | M | Low | 3, 6 |
| 9 | **Ticket write capabilities.** `TicketCloser`, `TicketClaimer`, `TicketNoter`, `CardMover` (retiring the `jira()` cast) and `ProviderInfo` field labels (B3–B5). Supersedes LEG-401 refactor 3. | M | Medium | — |
| 10 | **Generic `ticket.close`** (and claim and note) workflow steps; `burnlite.task.close` becomes a deprecated alias; `ticket-to-merged-pr` uses `ticket.close`. Coordinate with LEG-488 for list and import. | S | Low | 9 |
| 11 | **Agent ticket tools** (`ticket_*` in `legato mcp serve`); the handoff brief offers only the verbs the source supports (B6). | M | High | 9 |
| 12 | **Bundled defaults.** Download pinned `st` and `yg` builds into `<data_dir>/bin`, checked against `SHA256SUMS` as `selfupdate` does, or default to git when they are absent (open question 1). Doctor and setup stop naming them unless the user chose them. | M | High | 2, 7 |
| 13 | **Second ticket provider: GitHub Issues**, read and close. | L | High | 9, 10 |
| 14 | **Move the operator's managed skills and macros** (B8, B9a, B9b: implement-stack, proposal-stack, the address-stack-feedback skill, "Ticket to PR" and "Address Stack Feedback") to Legato verbs. Operator content, not repository code: a short guide and the edits at the host. | S | Low | 8, 11 |
| 15 | **Docs.** Generic site pages "Stacks" and "Worktrees", each with a backend section; `integrations/staccato` and `integrations/yggdrasil` become backend pages. Each earlier ticket updates its own pages, as `CLAUDE.md` asks; this is the final sweep. | S | Low | 8, 12 |

Total: about 56 points.

Tests each ticket needs:

| Ticket | Tests |
| --- | --- |
| 1 | A factory test for each `worktrees.tool`; a config test that the alias maps to the new keys; `go test ./config` (the configuration page names the new keys). |
| 2 | Real `git` in `t.TempDir()`: add, provision, remove, the merged-branch rule. |
| 4, 7 | One table of stack scenarios (append three, edit the bottom, restack, sync with a refused push) that gives the same `Stack` from both backends. |
| 5 | Golden documents, and a handoff test across a feature-negotiated pair (`internal/handofftest`). |
| 9, 10 | The existing fake Burnlite and a fake Jira; `ticket.close` skips when optional and fails with "<Provider> cannot close tickets" otherwise. |
| 8 | The prompt text has no `st ` or `yg ` when the git backends are set. |

## Open questions

Each has a recommendation. They shape the tickets above, not this audit.

1. **Bundle `st` and `yg`, or default to git?** The milestone says the
   bundled defaults are Staccato and Yggdrasil. *Recommend:* `stacks.tool`
   and `worktrees.tool` default to `auto`: the bundled tool when Legato has
   downloaded it, else git. Legato downloads pinned builds on first use
   (ticket 12). This decides ticket 12's scope.
2. **The operator's macros and managed skills (B8, B9a–B9d) live outside
   the repository.** Of the four macros, only "Ticket to PR" (Burnlite) and
   "Address Stack Feedback" (Staccato, through its skill) leak.
   *Recommend:* ticket 14 changes those two macros and the
   `address-stack-feedback` skill; "Rebase and repush" and "Fix CI" need
   nothing.
3. **Where plain-git worktrees go.** *Recommend:* beside the primary
   checkout (`<primary>/../<repo>.worktrees/<branch>`), as `yg` does by
   default.
4. **Is the `.yggdrasil.toml` fallback a leak?** *Recommend:* no. Keep it for
   compatibility; it is read, never required (Y10).
5. **Agent ticket verbs: Legato tools, or the tracker's own MCP and the
   tracker doc?** *Recommend:* both. Legato's `ticket_*` tools cover the five
   core verbs, and skills and macros use them. The tracker's MCP stays for
   richer operations, and the tracker doc for planning skills. Acting
   through the board's connection on an agent's behalf may need a
   credential-broker rule, which is why ticket 11's uncertainty is high.
6. **Overlap with LEG-400 and LEG-401.** Refactors 3 and 7 are tickets 9 and
   1 here. *Recommend:* LGPOL owns them, with a note on LEG-401 when the
   tickets are filed.
7. **Should the stack handoff wire format change?** It touches workers
   already paired. *Recommend:* a new feature flag in handoff v2's
   negotiation, keeping Staccato's document for an older peer (ticket 5).

## Out of scope

- Any code, site or `docs/ai` change, and filing tickets.
- The forge abstraction (GitHub hard-wiring), the agent-kind descriptor, and
  the native-bridge and `chimera_*` naming (LEG-401 refactors 1, 2 and 9).
- Chimera as a backend. It is an agent tool, and agent tools have a seam.
- Legato's **own** use of Burnlite in its development: `CLAUDE.md`,
  `docs/agents/issue-tracker.md`, and `LEGATO_HOLDER` in that doc. That is
  the project's tracker, not the product.
- The browser extension's provider list
  (`browser-extension/src/ticket-providers.ts`): legit provider naming.
- Mentions in `docs/ai/*`: internal docs, which follow the code as tickets
  land.
- Swarm sub-task IDs `st-…` (`internal/engine/hooks/prompts/conductor.md`):
  not Staccato.

## Appendix: greps that reproduce the audit

Run at the repository root.

```bash
# Go, not tests
grep -rnE '"st"|\bst (append|restack|sync|log|graph|pr|continue)|[Ss]taccato|stackgraph' --include='*.go' . | grep -v _test.go
grep -rnE '"yg"|[Yy]ggdrasil|\byg |NewRunner\(' --include='*.go' . | grep -v _test.go
grep -rniE 'burnlite' --include='*.go' . | grep -v _test.go
grep -rn 'jiraSource)' internal/service --include='*.go' | grep -v _test.go
# Workflows
grep -nE 'burnlite|Yggdrasil' internal/engine/workflow/catalog.go internal/engine/workflow/examples/*.yaml
# Web UI
grep -rnE 'st graph import|Yggdrasil|Staccato' web/src --include='*.ts' --include='*.tsx' | grep -v test
# Site and README
grep -rliE 'staccato|yggdrasil|burnlite|\bst (sync|pr|graph)|\byg ' site/src/content/docs README.md
# Managed skills (inside an agent session)
for d in $(echo "$CLAUDE_CODE_PLUGIN_DIRS" | tr ':' ' '); do
  grep -rnE '\bst |[Bb]urnlite|claim_task|close_task' "$d"/skills 2>/dev/null
done
```
