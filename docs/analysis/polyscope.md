# Polyscope Competitive Analysis

**Date**: 2026-04-02
**URL**: https://getpolyscope.com/
**By**: Beyond Code (Germany, est. 2017, Laravel ecosystem background)
**Platform**: Native macOS only (13.3+)
**Pricing**: Free (Hobby), $99/yr (Pro), $299/yr (Team, 10 seats)

## What Polyscope Is

An "agent-first development environment" for orchestrating multiple AI coding agents in parallel. Supports Claude Code, OpenAI Codex, and Cursor CLI as backends. Local-first (SQLite at `~/.polyscope/`), GitHub-authenticated.

## Key Features

### Workspace Isolation Model
- **Copy-on-write repo cloning** -- each agent gets a fully isolated filesystem clone, not just a tmux session in the same repo.
- **Per-workspace branches** -- automatic branch creation per task.
- **Disposable** -- workspaces are treated as throwaway; one workspace per feature/fix.

### Agent Orchestration
- **Autopilot** -- takes a high-level goal, decomposes it into sequenced user stories with acceptance criteria, then executes them sequentially with cross-story context (`.context/progress.md`). Pause/resume, drag-and-drop reordering, inline story editing.
- **Opinions** -- queries multiple AI models in parallel on the same question, then synthesizes a consensus (agreement, divergence, recommendation).
- **Linked workspaces** -- read-only cross-workspace context sharing so one agent can reference another's work.
- **Supervisor pattern** -- a dedicated workspace that monitors/guides other workspaces.

### Visual / Rich Interaction
- **Visual element editor** -- WYSIWYG crosshair selector on a live web preview; click an element, describe what to change, agent modifies source code.
- **Built-in preview browser** -- renders the app with nav, URL bar, JS console.
- **Diff panel** -- syntax-highlighted unified/split diff view with inline commenting.
- **Image/screenshot attachment** -- drag-and-drop images into prompts.

### Review System
- **Dedicated review agent channel** -- separate from the implementation chat, with built-in review instructions and per-repo review preferences.

### GitHub Integration
- **Issue-to-workspace** -- create a workspace directly from a GitHub issue, pre-populated with context.
- **PR-to-workspace** -- open an existing PR as a workspace for review/iteration/CI fix.
- **CI status in activity feed** -- GitHub Actions status shown inline with auto-fix capability.
- **One-click PR creation** -- agent commits, pushes, creates PR from workspace.

### Configuration
- **`polyscope.json` project config** -- setup/archive lifecycle scripts, run scripts with dedicated terminal tabs, reusable prompt templates ("tasks"), preview URL config.
- **Reusable task templates** -- predefined prompts (security review, test generation, etc.) that auto-create a workspace and send the prompt.

### Other
- **Per-prompt model switching** -- choose between Claude, Codex, Cursor per workspace or per prompt.
- **E2E encrypted remote/mobile access** -- connect from any browser to monitor and guide agents (Pro tier).
- **Multi-repo workspaces** -- link workspaces from different repositories for cross-repo agent collaboration.
- **Nightwatch integration** -- Laravel error monitoring to workspace pipeline.
- **Laravel Herd integration** -- auto-detected dev environment setup.
- **Command palette** (Cmd+K) -- fuzzy search across commands, workspaces, branches.

## Gap Analysis: What Polyscope Has That Legato Does Not

| Area | Polyscope | Legato |
|------|-----------|--------|
| Workspace isolation | Copy-on-write repo clones | Shared filesystem via tmux |
| Task decomposition | Autopilot (goal -> stories -> sequential execution) | Manual task creation only |
| Multi-model consensus | Opinions (parallel query + synthesis) | Single agent per task |
| Cross-agent context | Linked workspaces (read-only) | Agents fully independent |
| Visual editing | WYSIWYG element selector on live preview | Terminal only |
| Preview browser | Built-in with nav, JS console | None |
| Diff view | Syntax-highlighted unified/split with inline comments | Raw tmux capture-pane |
| Image prompts | Drag-and-drop screenshots | Text-only via tmux |
| Review workflow | Dedicated review agent channel | None built-in |
| GitHub issues | Issue-to-workspace with context injection | No GitHub Issues integration |
| PR-as-workspace | Open PR for review/iteration | PR tracking only (read-only) |
| CI auto-fix | Agent sees CI failures, can auto-remediate | Shows check_status on cards only |
| Project config | `polyscope.json` with lifecycle scripts + prompt templates | `config.yaml` (no per-project scripts or templates) |
| Model switching | Per-prompt model selection | Single agent type per session |
| Remote access | E2E encrypted browser access | Local only (tmux attach) |
| Multi-repo | Cross-repo workspace linking | Single repo per board |

## Gap Analysis: What Legato Has That Polyscope Does Not

| Area | Legato | Polyscope |
|------|--------|-----------|
| Ticket providers | Jira bidirectional sync with conflict resolution | GitHub-only |
| Task management | Kanban board with columns, priorities, workspaces, archiving | Flat workspace list |
| Platform | Terminal-native, runs anywhere (Linux, macOS, SSH) | macOS only |
| Multi-instance | IPC broadcast via Unix sockets | Appears single-instance |
| Activity tracking | Structured working/waiting/idle states with duration metrics | Activity feeds, no structured state tracking |
| CLI scripting | `legato task update/note/link`, `legato agent state/summary` | Laravel SDK, no general CLI |
| Tmux integration | Native tmux session management with status line | No tmux awareness |
| AI tool hooks | Hook-based activity state tracking (Claude Code, Staccato) | No hook system |

## Strategic Takeaways

The biggest conceptual gap is **workspace isolation** (copy-on-write clones vs shared filesystem) and **orchestration features** (Autopilot decomposition, Opinions multi-model, linked workspaces). Polyscope treats the agent workspace as a first-class isolated environment; Legato treats it as a tracked tmux session.

Legato's strengths are in **task management** (kanban, Jira sync, priorities), **platform reach** (terminal-native, Linux), and **observability** (agent state tracking, duration metrics, IPC). These are areas Polyscope is weak or absent.

Potential areas to explore:
- **Git worktree-based isolation** -- lighter than CoW clones, achievable without macOS APFS
- **Task decomposition** -- breaking a goal into subtasks with sequential or parallel execution
- **Cross-agent context sharing** -- letting one agent read another's output
- **Richer diff/review integration** -- beyond raw capture-pane
- **GitHub Issues as a ticket provider** -- alongside Jira
