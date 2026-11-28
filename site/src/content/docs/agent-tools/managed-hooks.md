---
title: Managed hooks
description: Keep hooks such as a CodeEval quality gate in one folder, and Legato gives them to the agents it starts, with a switch to turn each off.
sidebar:
  order: 66
---

A managed hook is a hook you keep in Legato's managed-hooks folder. Legato gives it to the agents it starts, translated for each agent tool, and it is on until you turn it off. Use one when every agent should meet the same check, such as CodeEval's net-debt gate when an agent ends its turn.

A hook in `~/.claude/settings.json` reaches every Claude Code session on the machine, including ones Legato did not start, and no agent of another tool. A managed hook reaches the agents Legato starts, and you can turn it off without deleting it.

:::caution
Managed hooks arrive in stages. Read [Phase 1 limits](#phase-1-limits) before you rely on one.
:::

## Add a hook

Put a folder in `~/.legato/managed-hooks` with a `HOOK.yaml` and the scripts it runs:

```text
~/.legato/managed-hooks/
  codeeval/
    HOOK.yaml
    stop-check.sh
```

A link to a folder elsewhere works too. CodeEval's release has a ready `legato-hook/` folder: link it in as `codeeval`.

```bash
ln -s /path/to/codeeval/legato-hook ~/.legato/managed-hooks/codeeval
```

Legato reads the folder each time it lists hooks and each time it starts an agent, so a hook you add, change or remove counts at once: no restart. Legato never writes into the folder.

To keep hooks somewhere else, set `managed_hooks.dir` (it takes effect at the next restart):

```yaml
managed_hooks:
  dir: ~/Projects/my-hooks
```

## Write HOOK.yaml

```yaml
name: codeeval
description: CodeEval net-debt gate at the end of each turn
agent_tools: [claude-code, chimera]
resources: [codeeval]
events:
  stop:
    command: ./stop-check.sh
    timeout: 120
```

- `name` is the folder's name: lowercase letters, digits and hyphens, at most 64 characters.
- `description` says what the hook does. Settings shows it.
- `events` names what to run and when. Each event has a `command`, run from the hook's folder, and an optional `timeout` in seconds.
- `agent_tools` limits the hook to some agent tools: `claude-code`, `chimera`, `codex`. Leave it out for every tool Legato can give hooks to.
- `read_only: true` gives the hook to read-only agents and reviewers too. They get no managed hooks otherwise.
- `resources` names [resources](/legato/agent-tools/resources/) the hook's scripts need. An entry is a name, or `{name: ..., as: ...}` when the name has characters other than letters, digits and `_`.
- `yields_to` names the same check in a repository's own hooks, so that the repository's runs instead of yours: `claude_code_command` is a regular expression matched against the commands of a repository's Claude Code hooks, `chimera_hook` a glob matched against the file names in its `.chimera/hooks/` folders.

| Event | When it runs | Default timeout | Longest timeout |
| --- | --- | --- | --- |
| `session_start` | The agent's session starts | 10 s | 30 s |
| `pre_tool` | Before a tool call | 10 s | 30 s |
| `post_tool` | After a tool call | 30 s | 120 s |
| `stop` | The agent ends its turn | 120 s | 600 s |

`pre_tool` and `post_tool` may take `match`, a list of the tool calls they run for: `shell`, `file_edit`, `file_read`, `search`, `web`, or `mcp:<server>` for an MCP server's tools. Without `match` they run for every call.

Legato checks the whole file. A hook with any mistake, such as a timeout over the longest, an unknown event or matcher, a bad pattern in `yields_to` or an unknown agent tool, is listed with the mistake and given to no agent. Hooks beside it are not affected.

## Turn hooks on and off

**Settings › Agents › Managed hooks** lists every hook in the folder with its description, events, agent tools and path. A new hook is on. Turn one off with its **Give agents** switch, and agents started from then on do not get it. A hook with a mistake is marked **error** with what is wrong and has no switch; fix the file and it is listed as a hook at the next look.

Agents that are already running keep the hooks they started with.

The Reeve can turn a hook on or off too: it proposes the change, and you approve it.

## Which agents get a hook

Each time Legato starts an agent, it checks every hook in the folder and records which the agent has, and why each other one is off. A hook is off if any of these says so, and every reason that applies is recorded:

| Reason | When |
| --- | --- |
| `off:global` | You turned the hook off in Settings. |
| `off:invalid` | `HOOK.yaml` has a mistake. |
| `off:unsupported-tool` | The hook's `agent_tools` leaves out the agent's tool, or Legato cannot give hooks to that tool yet. |
| `off:spawn` | You left the hook out when you started the agent. |
| `off:read-only` | The agent may look but not change its checkout, and the hook does not say `read_only: true`. |
| `off:reviewer` | The agent reviews a pull request, and the hook does not say `read_only: true`. |
| `off:reeve` | The agent is the Reeve. `read_only: true` does not change this. |
| `off:find-out` | The agent works on a find-out task handed from another Legato. |
| `off:workflow` | A workflow started the agent. Workflow agents get no managed hooks for now. |

To leave a hook out of one agent, untick it in the web UI's [spawn dialog](/legato/using/agents/#the-web-spawn-dialog), which shows a ticked box for each hook that agent would get with the tool you picked (none for a tool that cannot run managed hooks). Or start the agent with `hooks_off`, a list of hook names, on `POST /api/agents/spawn` or `POST /api/tasks/{id}/agent`.

The hooks you leave out stay out when the agent is started again: reloaded, switched in or out of the sandbox, resumed after a crash or started new in its place, restored or started fresh from the session history. Everything else is worked out again then, so a hook you turned off in Settings meanwhile is off, and one you turned back on is on. A name that is no hook is ignored, with a note on the task. `GET /api/agents/{task_id}/managed-hooks` lists what an agent has and why the rest are off. An agent started before managed hooks existed has none recorded.

## What agents get

| Tool | Gets managed hooks |
| --- | --- |
| Claude Code | Yes, as a plugin named `legato-hooks` |
| Chimera | Not yet: skipped with a note on the task |
| Codex | Not yet: skipped with a note on the task |

When a Claude Code agent starts, Legato copies each hook it has into a folder of that agent's own, so a hook you change later reaches the agents started after the change, and an agent that is reloaded. Only what is inside the hook's folder is copied: a link to something outside it is left out. Scripts stay executable.

Each hook's command runs from its copy, with these variables:

| Variable | What it holds |
| --- | --- |
| `LEGATO_HOOK_NAME` | The hook's name. |
| `LEGATO_HOOK_DIR` | The hook's copy. It is read-only. |
| `LEGATO_HOOK_STATE_DIR` | A folder for the hook to keep state in during the agent's session: `/tmp/legato-hooks/<execution>/<name>`. |
| `LEGATO_HOOK_SEED` | A JSON file saying where the agent's checkout was when it started (below). |

The seed file holds the commit checked out when the agent started (`head`), its branch, the repository, the task, the agent's execution, its folder and the time:

```json
{"version": 1, "head": "3f2c…", "branch": "feature", "repo": "github.com/acme/widget",
 "task_id": "a1b2c3d4", "execution_id": "ex-…", "work_dir": "/home/me/widget", "time": "2026-11-29T10:00:00Z"}
```

A hook such as CodeEval's compares the working tree with `head`, so it judges only what the agent changed. An agent that is restarted or resumed is a new start: its seed names the commit at that moment.

A hook that fails, runs out of time or cannot start lets the agent carry on.

## Phase 1 limits

- Only Claude Code agents get managed hooks.
- Agents that a workflow starts get none.
- `pre_tool` and `post_tool` hooks are checked and listed, but not run yet. An agent given a hook that has them gets a note on its task.
- There are no per-repository offs and `yields_to` is not acted on yet. A repository whose own `.claude/settings.json` already runs CodeEval's Stop hook, or a `~/.claude/settings.json` that does, runs it twice: once there and once as the managed hook. The code-evaluator repository is one. Untick `codeeval` when you start the agent, or send `hooks_off: [codeeval]`.
- A `disableAllHooks` setting in Claude Code turns the gate off without a warning.
- Legato does not yet warn you when an agent's hooks never ran.
- Only the web spawn dialog has the boxes; the TUI's spawn dialog does not.
