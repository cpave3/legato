# Engram sessions

An **engram** is one named individual that the engram CLI keeps (argus, hestia, mneme, …), with its own memories, open threads and handoffs. Legato can start an agent session **as** an engram: the engram's boot packet is in the session's launch prompt, the session's own engram writes work inside the sandbox, and the choice is remembered per repository and per task. A normal session, with no engram, stays one choice away.

The spec is Burnlite LEG-46; user documentation is the site page `agent-tools/engrams`. Legato writes no guidance of its own about keeping an engram current: the boot packet's practice section is that guidance.

## What Legato needs of engram

Legato is written against the contract of Burnlite LEG-107, which must land in the engram CLI (`github.com/cpave3/engram`) before engram sessions work:

- `engram where [--dir <path>] --json` needs no identity and starts no session. It prints `{version, binary, store: {root, source, exists}, place?: {primary, scopes}}`; `source` is `ENGRAM_HOME`, `XDG_DATA_HOME` or `default`; `place` only with `--dir`, `primary` first in `scopes`.
- `engram identity list --json` prints `[{name, created, summary}]` (it already does).
- Exit codes past `0`/`1`/`2`: `3` unknown identity, `4` missing store (its root folder does not exist), `5` identity already exists (`identity new`).
- `engram session end` marks the ambient session over without a handoff; a strong key (`ENGRAM_SESSION`) still resumes it.
- The boot packet has a practice section, never cut.

Until a release of engram has these, the probe fails (`where` is an unknown command) and Legato offers no engrams.

## The client: `internal/engine/engram`

A client of the CLI that knows nothing of Legato and imports no other engine package. `Runner{Command, LookPath, Env, ProbeTimeout, BootTimeout}` runs the binary with **only** `Env` plus what a call adds, never Legato's own environment; `HostEnvironment(getenv)` is the part of Legato's environment engram needs to find its store and git (`HOME`, `PATH`, `XDG_DATA_HOME`, `ENGRAM_HOME`), so a Legato started inside an engram session does not lend its `ENGRAM_IDENTITY` or `ENGRAM_SESSION` to the engrams it runs.

- `Probe` (`where --json`) and `List` (`identity list --json`), 2 s each.
- `Place(dir)` (`where --dir <dir> --json`): primary and scopes.
- `RepoKey(host, owner/name)`: `repo:` + lowercase `host/owner/name` (host `github.com` when empty), for a job clone that does not exist yet. `KindOf` (`repo` or `folder`) and `Label` (the key without its prefix).
- `Boot(env, dir)`: the packet from stdout, stderr apart (`BootResult`), 5 s. Exit 3 is `ErrUnknownIdentity`, exit 4 `ErrMissingStore`, any other failure an `*Error{Code, Message}`, the deadline `ErrTimeout`.
- `EndSession(env, dir)`: `engram session end`.
- A missing binary is `ErrNotInstalled`.

## Settings and detection

`engram.enabled` (`auto`, the default; `on`; `off`) and `engram.command` (the binary; `engram` on `PATH` when empty) are live settings in the registry (`config/settings.go`), so a change applies to the next probe.

`service.EngramService` (`internal/service/engram.go`) reads both from the effective configuration on every call (`EngramOptions.Config`) and makes the client with `EngramOptions.Runner` (`EngramRunner(os.Getenv)` in production). `Status(ctx)` probes: `where --json`, then `identity list --json`.

- `off`: no probe; `Enabled` false.
- `auto`: `Enabled` when both answer; a failure only leaves engrams out (its words are in `Error`).
- `on`: the same, and a failure is also logged as an error.

Nothing is cached between calls, so installing engram later needs no restart. `cmd/legato/engram_runtime.go` (`engramsFor(db)`) holds one service per store and runs the startup probe.

## API

`GET /api/engrams` (`internal/server/engrams.go`): `EngramStatus` — `{enabled, mode, found, binary, version, store, store_source, store_exists, error, engrams: [{name, summary}]}`. Without an engram service it answers `enabled: false`.

## The spawn, with an engram

`AgentSpawnOptions.Engram` names the engram; `EngramSession` is the session a resumed conversation keeps (found with `store.LastEngramSession` for the task and engram when empty); `EngramRequired` fails the spawn when engram does not list the engram (a workflow step's). The agent service gets the `AgentEngrams` (the `EngramService`) through `SetEngrams`, which `giveAgentsEngrams` calls wherever `cmd/legato` makes an agent service, except the swarm CLI's. `bootEngram` (`internal/service/agent_engram.go`) runs in `SpawnAgent` after the job clone exists and before the prompt files are written:

1. **Refused** for swarm agents (a role, parent or sub-task).
2. **Session key**: a fresh start uses the new execution's ID; `SessionPolicy == "resume"` (any tool, Chimera included) reuses the saved one.
3. **`EngramService.Boot`**: `where` (its binary and store); a store whose root does not exist is a missing store; the engram must be one `identity list` lists. The environment is `ENGRAM_IDENTITY`, `ENGRAM_HOME` (the store root from `where`), `ENGRAM_SESSION`, `ENGRAM_HARNESS` (`EngramHarness`: `claude-code`, `codex` or `chimera`; any other tool is refused), `ENGRAM_MODEL` (only with a chosen model), `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID`, set alike for the boot and in the session's `envMap`. On a resume nothing is booted; otherwise `engram boot` runs, with the binary `where` named, in the agent's working directory (the clone, for a clone spawn), 5 s.
4. **Launch prompt**: `EngramPromptSection(name, packet)` comes first, before every other section, for all three tools. No `--budget` is passed.
5. **Record**: `store.SetExecutionEngram` writes `executions.engram` and `engram_session`.
6. **Failure**: an error, the timeout, an engram engram no longer lists, a missing store or engram not found starts the agent without the engram: no `ENGRAM_*`, no packet, nothing on the execution. `SpawnReport.EngramWarning` (`WithSpawnReport` passes one through the context) says why, in `EngramFailure`'s words, and the spawn endpoints answer it as `engram_warning`. A spawn that fails after a fresh boot runs `engram session end`.

`POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` take `engram` (a name, or empty for none) and answer `engram` or `engram_warning` (`spawnResult` in `internal/server/agents.go`).

## In the sandbox

`SandboxEngram{Binary, BinMount, Store}` is a per-spawn input of the sandbox plan, like `SandboxGH`, and does not depend on `default_mounts`. `sandboxPrefix` gets the booted engram's binary and store (from `where`), makes `<runtime>/legato/sandbox/<execution>/bin/engram` as an empty file, and returns the launch line with `PATH=<that bin folder>:"$PATH"` before the backend, so the shell that runs it keeps its own `PATH` behind the engram's folder. `BuildSandboxPlan` binds the binary read-only at `BinMount` and the whole store read-write after `check`; a store that does not exist refuses the sandbox, and so the spawn. The binary is the real one `where` names (`os.Executable()` in engram), never a mise shim, and no other `engram` on the host's `PATH` is covered: links there are resolved, and covering a shim would cover mise itself. `ENGRAM_HOME` is the same path inside and out.

Host and sandboxed processes write the same files. Checked against engram `main` (`b51ee38`): 12 concurrent `engram note` calls on one session and 12 concurrent `engram remember` calls all landed.

## A boot that fails

The agent starts without the engram (see § The spawn, with an engram) and the person is told three ways:

- **Where it was spawned.** The spawn endpoints answer `engram_warning`, which the web dialog's callers show as an error toast. A spawn that runs in the background (a clone) has no answer to carry it; the Attention item is what says so. The TUI shows it in its status bar (see [TUI](tui.md)).
- **Attention.** `bootEngram` returns a `spawnedEngram` with `failed` set, and `SpawnAgent` records it with `store.SetExecutionEngramFailure` (`executions.engram_boot_failed`, `engram_boot_error`; migration 105). The `engram` producer (`EngramAttentionAdapter`, `internal/service/engram_attention.go`) lists the running executions with one as `engram_boot_failed` items on their task, reconciled on `EventAgentStarted` and `EventAgentDied`, so the item goes when the agent ends.
- **The log**, with engram's stderr.

A spawn that fails after its engram booted runs `engram session end` (`spawnedEngram.endIfFresh`); a resumed session is left as it was.

## Restart and ending

- **Restart** (`restartAgent`) reads `engram` and `engram_session` from the execution it replaces into `AgentSpawnOptions.Engram` and `EngramSession`. A resumed conversation keeps both and gets no packet; a restart that does not resume boots again with a new key (the new execution's ID).
- **Ending.** `EndExecution`, where every agent's execution ends (a kill, a pane that went away, a task deleted, archived or handed back, a spawn that failed), runs `engram session end` for an execution with an engram (`endEngramSession`), in its working directory when that still exists. A restart that resumes stops its agent with `withEngramSessionKept`, so the session goes on in the agent that resumes it. Legato never asks the agent for a handoff; engram's practice section does. An ended session still resumes through its strong key.

## Bindings, Remember and pre-selection

Bindings live in `engram_bindings` (see [Database](database.md) § Engram sessions), never in `config.yaml`.

**Place key.** `EngramService.place` (`internal/service/engram_picker.go`):

- A folder (`Dir`): `engram where --dir`. A binding matches when its key is any of the folder's scopes, and a `repo:` match beats a `dir:` one; the place is the matching key, else engram's primary key. A folder outside git has a `dir:` key and kind `folder`. A folder engram cannot name has no place.
- A job clone (`Repo`), which does not exist when the dialog opens: `engram.RepoKey("", owner/name)`, `repo:github.com/<lowercase owner/name>`. Legato's clones are of GitHub only today; the host would come from the credential profile's `git` if that changed.

**Picker** (`EngramService.Picker`, built once for the TUI and the web): `EngramPicker{EngramStatus, Place{key, label, kind, default}, Engrams[{name, summary, mark, bound}], Preselect}`.

- Pre-selected: the task's last engram (`store.LastTaskEngram`: the engram of the task's latest execution that ran as one), else the place's default, else none.
- Order, each once, after No engram: the task's last engram (`mark: last`), the default (`mark: default`), the other bound engrams by `last_used_at` (newest first), then every other engram A–Z.
- An engram `identity list` does not list is left out; a default that is gone falls back to none. Its binding is kept unchanged, and works again if it comes back. `place.default` still names it, so it can be cleared.

**Remember** (`EngramService.Remember`): with an engram, binds it to the place if it is not bound and makes it the default (the old default stays bound); with none, clears the place's default (the bound engrams stay). Not ticked, nothing is stored. The spawn endpoints take `remember_engram` and call it before the spawn (`rememberEngram` in `internal/server/engrams.go`); a Remember that cannot be done (a job clone named only by its ID) is logged and does not stop the spawn.

**Last use.** `EngramService.Touch(dir, name)` sets `last_used_at` on each binding of the engram to one of the directory's scopes; `bootEngram` calls it after every successful boot.

`GET /api/engrams?dir=…|repo=…&task_id=…` answers the picker; `dir` may start with a tilde. Workflows never read or write bindings.

## Settings

`EngramService.Bindings` lists every binding by place (`EngramBindingsView{Status, Places}`), sorted by label, whether engram is found or not; an engram `identity list` does not list is `missing` (never while engram cannot be asked). `Unbind` and `SetDefault` (an empty name clears the default) change them. The web UI's Settings › Agents › **Engram bindings** shows them; see [Web UI](web-ui.md).

## In the TUI

The spawn overlay's `engram` row (see [TUI](tui.md) § Spawning an agent) shows the same `EngramService.Picker`: the TUI asks the service directly, not the API.

## Display

`store.AgentSession.Engram` comes from the agent session listing's join (`COALESCE(e.engram, '')`), and `service.AgentSession.Engram` from it; `GET /api/agents` answers `engram`. `RelatedAgentSummary.Engram` names the engram a task's agents run as (empty when they run as different ones), which the web board answers as `agent_engram` and the TUI board's panel shows in an `engram` row. The tool is shown as `claude · hestia` (TUI) and `Claude Code · hestia` (web) wherever it is shown: see [TUI](tui.md) § Agents and [Web UI](web-ui.md) § An agent's engram. The report's executions table has an **engram** column (`ExecutionBreakdown.Engram`, `by_execution[].engram`, and the Markdown export); there is no breakdown by engram.
