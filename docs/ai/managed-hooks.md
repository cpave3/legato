# Managed hooks

Legato keeps a folder of managed hooks: hooks the operator writes once, which Legato gives to the agents it starts, translated for each agent tool, on unless something turns them off (spec LEG-687; Phase 1 milestone tickets LEG-693 to LEG-704). The first is CodeEval's net-debt gate at the end of each turn. Site page: `agent-tools/managed-hooks`.

A managed hook is never a repository's own hook (`.claude/settings.json`, `.chimera/hooks/`) nor one in the operator's agent-tool settings (`~/.claude/settings.json`): those reach every session of their tool, and Legato neither reads nor changes them here.

## Phase 1 scope

Phase 1 (this milestone) delivers managed hooks to Claude Code, with a global off switch. What exists so far (LEG-694):

- the setting `managed_hooks.dir` and the folder;
- the scan and full validation of `HOOK.yaml` (`internal/engine/managedhooks`), including what Phase 1 does not act on yet, so a manifest valid today stays valid later;
- the global on/off state (`managed_hook_states`);
- Settings › Agents › Managed hooks and its routes;
- the policy (LEG-695): every spawn decides and records which hooks its agent has and why each other is off, takes `hooks_off`, and notes a tool that cannot run them (§ The policy);
- the Claude Code bundle (LEG-696): a `legato-hooks` plugin per execution whose entries call the runner, with each hook's copy and `hook.env`, the seed, the context and the index (§ The bundle).
- spawn offs (LEG-702): a checkbox per hook in the web spawn dialog, and the offs chosen at spawn kept through restart and interrupted resume (§ Spawning and relaunches).

The runner itself (`legato agent managed-hooks run`) is LEG-698: until it lands, every entry finds no such command and passes (§ Entries fail open), so a Claude Code agent with hooks on behaves as one without. Next come resources and the sandbox (LEG-697), the runner (LEG-698), the runs op (LEG-699), the stop hold (LEG-700), warnings (LEG-701), spawn offs (LEG-702) and web visibility (LEG-703). Chimera, `pre_tool`/`post_tool` delivery, repository offs, workflows and handoff are Phases 2 and 3 (LEG-690, LEG-691).

## Phase 1 limits

Kept current by each Phase 1 ticket; the site page's section of the same name says the same.

- **Claude Code only.** Chimera and Codex agents get no managed hooks (`off:unsupported-tool`), with one skip note per execution.
- **Workflow agents get none** (`off:workflow`): any execution with a workflow attempt, until Phase 3's step-complete skip lands, when the reason is removed.
- **`pre_tool` and `post_tool` are not delivered yet.** They are validated, listed and shown, but only `session_start` and `stop` run: the bundle registers no entry for them and leaves them out of the index, and a spawn whose on hooks have any writes one launch note (§ The bundle).
- **No repository offs and no `yields_to`.** A repository whose own `.claude/settings.json`, or the operator's `~/.claude/settings.json`, already runs CodeEval's Stop hook runs it twice: once there, once as the managed hook. code-evaluator is one. The workaround is to untick `codeeval` at spawn, or send `hooks_off: [codeeval]`.
- **`disableAllHooks` silently turns the gate off,** with no warning.
- **No never-ran warning.**
- **The TUI's spawn overlay has no checkboxes,** only the web spawn dialog; and a workflow resume or a handed-off agent's restart carries no offs (Phase 3), since workflow agents are `off:workflow` and handoffs choose none yet.

## The managed-hooks folder

- **Where**: `managed_hooks.dir` (`config.ManagedHooksConfig`, `config/managed_hooks.go`), else `<legato home>/managed-hooks`: `~/.legato/managed-hooks`, or `$LEGATO_HOME/managed-hooks` (`swarm.LegatoHome`). `ResolveManagedHooksDir(cfg, legatoHome)`; `cmd/legato/managed_hooks_runtime.go` (`managedHooksDir`, through `legatoHomeFolder`, which `skillsDir` shares). A set value must be absolute or start with `~/` (`normalizeFolders`, which checks `skills.dir` the same way). Registered as a `risky` string with mode `restart`, as `skills.dir`: the folder decides what runs in every agent.
- **A hook** is a subfolder holding `HOOK.yaml` (`managedhooks.FileName`). A link to a folder counts (CodeEval's `legato-hook/` is linked in as `codeeval`); the hook is named, and listed, by the link. Files at the top and folders starting with `.` are not listed. Folder names follow the skills rule: lowercase letters, digits and hyphens, at most 64 characters.
- **Scanning**: `managedhooks.Scan(dir)` returns every subfolder as a `Hook`, sorted by name. A folder that is not a usable hook (a bad name, no `HOOK.yaml`, any manifest error) is returned with `Hook.Err` and nothing else but its name, path and any description read; it is given to no agent. The scan never fails over a hook: only an unreadable folder is an error, and a missing folder holds no hooks. The package knows nothing of Legato (ADR 1: stdlib, `gopkg.in/yaml.v3` and the leaf `subfolders`, whose `List` it shares with the skills scan).
- **Finding changes**: a rescan at every list and every spawn, as skills, so a hook added, changed or removed shows without a restart. Settings asks every 10 s and on window focus.
- **Legato never writes into the folder.**

## The manifest

```yaml
name: codeeval                        # must equal the folder's name
description: CodeEval net-debt gate at the end of each turn
agent_tools: [claude-code, chimera]   # optional; empty is every supported tool
read_only: false                      # true: also given to read-only slots and reviewers
resources: [codeeval]                 # named Legato resources; an entry may be {name, as}
yields_to:                            # skip when the repository already runs the same check
  claude_code_command: 'codeeval\s+check'   # RE2 against repository hook commands
  chimera_hook: '*codeeval*'                # glob against .chimera/hooks/<Event>/ file names
events:
  stop:
    command: ./stop-check.sh          # relative to the hook folder
    timeout: 120                      # seconds
  pre_tool:
    match: [shell, file_edit, "mcp:github"]
    command: ./guard.sh
```

Every rule is checked at scan time; the first broken one is the hook's error, prefixed `HOOK.yaml:`.

- **Unknown fields are errors** (the decoder's `KnownFields`), so a typo such as `timout` is caught rather than ignored.
- **`name`** and **`description`** are required; `name` must equal the folder's name.
- **`events`**: at least one of `session_start`, `pre_tool`, `post_tool`, `stop` (`managedhooks.Events()`, in that order). Each needs a `command`. Any other event is an error (`session_end`, `user_prompt` and `permission` are out of scope).
- **`timeout`** (seconds; `managedhooks.TimeLimit`): default/maximum `session_start` 10/30, `pre_tool` 10/30, `post_tool` 30/120, `stop` 120/600. Zero, negative or over the maximum is an error. `Handler.Timeout` holds the manifest's or the default.
- **`match`** only on `pre_tool` and `post_tool`: `shell`, `file_edit`, `file_read`, `search`, `web`, `mcp:<server>` (`<server>` letters, digits, `_` and `-`). Empty matches every tool call. The runner owns the table that translates them into each tool's names.
- **`agent_tools`**: names from `claude-code`, `chimera`, `codex` (`managedhooks.AgentTools()`). An unknown name is an error; a known one Legato cannot deliver to is not (it is an off reason at spawn).
- **`resources`**: each entry a name or `{name, as}`. `as` is the suffix of `LEGATO_HOOK_RESOURCE_<as>` and must be capital letters, digits and `_`; without it the name upper-cased must be one (else "give it an as"). Two entries with the same suffix are an error. `Resource{Name, As}`, `As` filled either way.
- **`yields_to`**: `claude_code_command` is compiled as RE2 (`YieldsTo.ClaudeCodeCommand`, a `*regexp.Regexp`), `chimera_hook` checked as a `filepath.Match` glob (`YieldsTo.ChimeraHook`, `MatchesChimeraHook(name)`). A bad pattern drops the whole hook. Acting on them is Phase 3.
- **`read_only`** (bool): opts the hook into read-only slots and reviewers, never the Reeve or a find-out.

## On and off

`managed_hook_states` (migration 163; `name` the folder name, `enabled`, `updated_at`), `store.ManagedHookStates` and `SetManagedHookEnabled`. A hook with no row is **on**: putting its folder in place is how the operator adds one. A row outlives its folder. `ManagedHookService.SetEnabled` (`internal/service/managed_hooks.go`) refuses a name with no usable folder (`ErrManagedHookNotFound`): absent, or listed with an error, which no state could give to an agent. This is the global layer (`off:global`) of the policy the spec describes; the other layers come with their tickets.

`ManagedHookListing` is the Settings view (name, description, events, agent tools, read-only, path, error, enabled) and carries the scanned `managedhooks.Hook` (`json:"-"`) for the policy.

## API and Settings

- `GET /api/settings/managed-hooks` (read) → `{dir, hooks: [{name, description, events, agent_tools, read_only, path, error?, enabled}], supported_tools: [...], repositories: []}`, hooks sorted by name. `supported_tools` names the agent tools that can run managed hooks: those whose adapter is a `HooksAdapter` (`service.ManagedHookTools` over the agent service's `RegisteredAdapters`/`AdapterFor`; Claude Code once LEG-696 lands, none before; always a list). The spawn dialog reads it, so the client never names a supported tool itself. `repositories` stays empty until repository offs (Phase 3).
- `PUT /api/settings/managed-hooks/{name}` (change) `{enabled: bool}` → the hook. `400` for a body without `enabled` or not JSON, `404` for no usable hook of that name (absent or errored), `503` without the service (`internal/server/managed_hooks.go`). Being a `change` route, the Reeve turns a hook on or off through a change set.
- **Settings › Agents › Managed hooks** (`web/src/components/settings/ManagedHooksSettings.tsx`, `#managed-hooks`, `web/src/lib/managedHooks.ts`): the folder, then a row per hook with its name, description, events, agent tools ("every agent tool" when empty, "read-only agents too" when `read_only`) and path, or its error marked **error**; a toggle **Give agents <name>** on valid hooks only. With none: "No managed hooks yet. Add a folder with a HOOK.yaml to <dir>." The registry row `managed_hooks.dir` sits in the same group. The list, its refresh and its calls are shared with Skills (`useFolderToggles`, `FolderItemRow`, `web/src/lib/folderItems.ts`).
- `cmd/legato/managed_hooks_runtime.go` builds `managedHookService(cfg, db)` for both web servers (`SetManagedHookService`).

## The bundle

At each spawn whose effective set is not empty, `giveCapabilities` gives the hooks as a capability kind of their own (`capabilityKinds.giveHooks`): the adapter's `HooksBundle` writes the tool's part and returns the bundle's folder, then `writeHooksContract` (`internal/service/managed_hook_bundle.go`) lays out the runner's part in it. The environment change (`CLAUDE_CODE_PLUGIN_DIRS`) is committed only once both are written; a failure of either is logged, removes only the bundle's folder, and the agent starts with its skills and without managed hooks. With no folder, an empty one, or every hook off, nothing is written and the environment is as before managed hooks.

This layout is **the contract** `legato agent managed-hooks run` (LEG-698) reads. Every file is version 1 and read-only (`0444`), every path in them absolute (the sandbox shows the capability folder at its own path, read-only):

```text
<data_dir>/capabilities/<execution>/claude-code/legato-hooks/
  .claude-plugin/plugin.json   Claude Code's: the plugin legato-hooks
  hooks/hooks.json             Claude Code's: the entries (below)
  index.json                   the hooks the agent runs; written last
  context.json                 the runner's legato object, fixed fields
  seed.json                    the checkout at this launch
  copies/<name>/               each hook's files (LEGATO_HOOK_DIR)
  env/<name>.env               each hook's hook.env
```

- **`index.json`** (`hookIndex`): `{"version": 1, "hooks": [{"name", "dir", "env", "state_dir", "events": {"<event>": {"command", "timeout"}}}]}`, sorted by name. `events` holds only the delivered events (Phase 1: `session_start`, `stop`; `deliveredHookEvents`), `timeout` in seconds (the manifest's, else the event's default). A hook with only undelivered events is listed with `events: {}`. A hook whose copy failed is left out, logged. `command` runs from `dir`.
- **`context.json`** (`hookContext`): `{"version": 1, "tool": "claude-code", "task_id", "execution_id", "slot", "role", "repo", "work_dir", "workflow": {"run_id", "attempt_id", "step_id"}, "seed": "<path of seed.json>"}`. `slot` is `main` for the main slot; `repo` the `host/owner/name` key, "" outside a repository Legato can name; `workflow` empty for an agent no workflow started (in Phase 1 every agent, since workflow agents get no hooks). The runner adds `event`, `stop_id`, `turn`, `stop_blocks_this_turn` and each hook's `state_dir` to make the spec's `legato` object.
- **`seed.json`** (`hookSeed`, pinned for CodeEval, CE-2): `{"version": 1, "head", "branch", "repo", "task_id", "execution_id", "work_dir", "time"}`. `head` is the commit checked out at *this* launch, `branch` its branch ("" when detached), both "" outside a git checkout; read as git's files by `gitrepo.HeadOf` (the work dir or the nearest folder above with a `.git`, a linked worktree's `.git` file followed to its git folder and `commondir`), so no git runs in the work tree. `time` is RFC 3339 UTC. A relaunch (restart, resume, Reload) is a new execution, so its seed is HEAD at the relaunch.
- **`env/<name>.env`** (hook.env): one `KEY=VALUE` line each, unquoted, the value everything after the first `=` (a value with a newline fails the bundle): `LEGATO_HOOK_NAME`, `LEGATO_HOOK_DIR` (the copy), `LEGATO_HOOK_STATE_DIR` (`/tmp/legato-hooks/<execution>/<name>`, G8; the runner makes it), `LEGATO_HOOK_SEED` (the seed's path). `LEGATO_HOOK_RESOURCE_<AS>` lines come with LEG-697.
- **`copies/<name>/`**: the hook's folder copied as a skill is (`skills.Copy`: from inside the folder only, links out left out, executable bits kept, 32 MiB at most).

**Claude Code's part** (`ClaudeCodeAdapter.HooksBundle`, `internal/engine/hooks/hooks_bundle.go`): the plugin `legato-hooks` (`hooks.HooksBundleName`) appended to `CLAUDE_CODE_PLUGIN_DIRS`, with `LEGATO_BIN` set if the session lacks it. `hooks/hooks.json` holds, each as one `matcher: ""` group:

| Claude Code event | Runner `--event` | Time limit |
| --- | --- | --- |
| `SessionStart` | `canary` (always; `hooks.HooksEventCanary`) | 10 s |
| `SessionStart` | `session_start` (when a hook has it) | longest `session_start` timeout + 15 s (`HooksEntryGrace`) |
| `UserPromptSubmit` | `turn_reset` (always; `hooks.HooksEventTurnReset`) | 10 s |
| `Stop` | `stop` (when a hook has it) | longest `stop` timeout + 15 s |

`canary` and `turn_reset` are internal events, never a manifest's. Any other event passed to `HooksBundle` is an error (the bundle fails); the service passes only delivered ones. **Launch note**: when an on hook has `pre_tool` or `post_tool`, one task note per execution (`noteUndeliveredHookEvents`): "Managed hooks' pre_tool and post_tool events are not delivered yet, so these run only at session_start and stop, if at all: <name> (<events>); …".

### Entries fail open

Each entry is:

```sh
[ -n "$LEGATO_BIN" ] || { cat >/dev/null; exit 0; }; out=$("$LEGATO_BIN" agent managed-hooks run --tool claude-code --event <event> --bundle '<bundle>' 2>/dev/null) || exit 0; printf '%s' "$out"; exit 0
```

It always exits 0, and passes the runner's stdout on only when the runner exited 0. Claude Code takes exit 2 as a block, and exit 2 is what today's `legato` answers for an unknown command (`unknown_command`, `exitUsage`: "unknown command \"agent managed-hooks\"" on stderr) and what a Go panic exits with; a missing `LEGATO_BIN`, a missing binary (127) or any other failure is a pass too (`TestAnEntryPassesUnlessTheRunnerAnswers` runs the entry against each). So **the runner (LEG-698) must**:

- take `--tool`, `--event` and `--bundle <folder>`, and read the tool's payload on stdin;
- block, or add context, only with Claude Code's JSON on stdout and exit 0 (`{"decision": "block", "reason": "…"}` for Stop; `hookSpecificOutput.additionalContext` for SessionStart), never with exit 2, which the entry turns into a pass;
- print nothing on stdout for a pass, and keep its diagnostics to stderr (dropped) and its reports;
- end within the entry's time limit (the event's longest hook timeout + 15 s).

## Off reasons

One vocabulary everywhere (records, API, panel, notes): `off:global`, `off:repository`, `off:step`, `off:spawn`, `off:handoff`, `off:read-only`, `off:reviewer`, `off:reeve`, `off:find-out`, `off:unsupported-tool`, `off:yields-to`, `off:missing-resource`, `off:invalid`, and in Phase 1 only `off:workflow` (`service.ManagedHookOff`, `Off*` constants in `internal/service/managed_hook_policy.go`).

## The policy

`HookSource` (`ManagedHooks(ctx, CapabilityRequest) (ManagedHookSet, error)`) decides an agent's managed hooks. It is separate from the skills' `CapabilitySource` but takes the same request; `ManagedHookService` implements it, scanning the folder at each ask, and `AgentServiceOptions.ManagedHooks` wires it (`cmd/legato`: `managedHookService(cfg, db)` beside `skillService` in every agent service). Every layer is evaluated on every hook and every reason recorded in layer order, so a hook can be off for several; any off wins (`decideManagedHook`).

| Layer | Phase 1 | Reason |
| --- | --- | --- |
| 1. Global | the hook's `managed_hook_states` row | `off:global` |
| 2. Manifest | an errored hook | `off:invalid` |
| | `agent_tools` without the agent's tool | `off:unsupported-tool` |
| 3. Repository | Phase 3 | `off:repository` |
| 4. Step | Phase 3 | `off:step` |
| 5. Spawn | a name in `hooks_off` (`AgentSpawnOptions.HooksOff`, `CapabilityRequest.HooksOff`) | `off:spawn`, chosen row `spawn` |
| 6. Handoff | Phase 3 | `off:handoff` |
| 7. Auto-off | a read-only checkout (`ReadOnly`), unless `read_only: true` | `off:read-only` |
| | the reviewer slot (`SlotReviewer`), unless `read_only: true` | `off:reviewer` |
| | the Reeve, always | `off:reeve` |
| | a find-out handoff (`HandoffPurpose`), always | `off:find-out` |
| | a workflow run or attempt on the spawn (Phase 1 only) | `off:workflow` |
| | a tool whose adapter is not a `HooksAdapter` (`ToolTakesHooks`) | `off:unsupported-tool` |
| 8. Launch checks | `yields_to` (Phase 3), a missing resource (LEG-697) | `off:yields-to`, `off:missing-resource` |

- The comments in `decideManagedHook` mark where the Phase 3 layers and the launch checks go.
- `off:unsupported-tool` is recorded once, whether the manifest or the adapter (or both) gave it.
- A delegation find-out is already off, being a read-only checkout. A handoff's purpose is the spawn's, else (main slot only) its card's open handoff's (`agentService.handoffPurpose`, `store.OpenHandoffForTask`), so a find-out agent restarted or resumed stays off.
- `ManagedHookSet.On()` is what `AgentCapabilities.Hooks` gets (`ManagedHook`: `Name`, `Path`, `Events`, and `Manifest`, the scanned `managedhooks.Hook` with handlers, resources and `yields_to`, for the bundle); `giveCapabilities` gives them through the `HooksAdapter`, a kind of their own beside skills (§ The bundle). Claude Code's adapter is one, so Claude Code agents record their hooks on; Chimera and Codex record `off:unsupported-tool` with the skip note.

**Records.** `agentService.decideManagedHooks`, run by `giveCapabilities` at each spawn once the execution exists, writes every hook's decision to `execution_managed_hooks` and each chosen off to `execution_managed_hooks_off` (see [Database](database.md) § Execution managed hooks). Only the chosen offs are a relaunch's to carry (LEG-702); the other layers are worked out again at each spawn. A hook turned off later leaves a running agent's record alone. `GET /api/agents/{task_id}/managed-hooks` reads the slot's newest session's execution, running or not, and once a stop has deleted that session, the slot's newest execution whose hooks a spawn decided, hooks or none (`execution_managed_hook_owners`), so a stopped agent's records stay readable and an older execution is never shown in place of a newer one with no hooks. Without a `HookSource`, or when it fails (logged), nothing is recorded and the agent gets no hooks.

## Spawning and relaunches

- **Spawn routes.** `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` take `hooks_off: [names]` (`AgentSpawnOptions.HooksOff`), recorded as `off:spawn` with chosen source `spawn`. An unknown name is ignored, with a note.
- **The web spawn dialog** (`SpawnAgentDialog`, `web/src/components/spawn/useManagedHooks.ts`, `ManagedHooksField.tsx`) lists `GET /api/settings/managed-hooks` each time it opens and shows a ticked checkbox (fieldset **Managed hooks**) for each hook that would apply to the chosen tool (`managedHooksFor` in `web/src/lib/managedHooks.ts`): no error, on globally, `agent_tools` empty or naming the tool, the tool in `supported_tools`, and `read_only: true` for a read-only agent (the dialog starts none today). It follows the tool picked (**Default** is the default adapter). No hook that applies, a `503` or an older server without `supported_tools`: no section. An unticked hook is sent as `hooks_off` (`SpawnAgentRequest.hooksOff`) by both callers, but only those unticked among the boxes shown for the tool sent, so one unticked under another tool is dropped. The server decides again at the spawn with the layers the dialog cannot see. There is no repository filter in Phase 1; the TUI's spawn overlay has no checkboxes.
- **Relaunches keep the chosen offs.** The offs of source `spawn` in the old execution's `execution_managed_hooks_off` (`agentService.keptHookOffs`) are the new spawn's `HooksOff` on: Reload and restart (`restartAgentWith`, the sandbox switch and host-network restart included), interrupted resume and its Start fresh (`ResumeInterruptedAgent`), and Restore and Start fresh from the session history (`RestoreConversation`, `StartFresh`); the last three through `runSpawnOptions`. So they are recorded again with source `spawn`. The global, manifest and auto-off layers are worked out again (story 48): a hook turned off globally meanwhile is off at the relaunch, one turned back on is on. A workflow resume and a handed-off agent's restart (`restartHandedOff`) carry none in Phase 1: workflow agents are `off:workflow` and handoffs have no offs (Phase 3).

**Notes** (task notes, source `legato`, one each per execution at most):

- a `hooks_off` name that is no hook in the folder: "hooks_off named no managed hook, so it was ignored: <names>." Nothing is recorded for it.
- the skip note, when the tool cannot run managed hooks and at least one hook would otherwise apply (`ManagedHookDecision.ToolSkipped`: off for no reason but the tool): "Managed hooks skipped for <tool>, which cannot run them yet: <names>. The agent started without them." No folder, or only hooks off for other reasons, writes none. There is no skills skip note, so Phase 1 never writes "skills and managed hooks skipped".

**Route.** `GET /api/agents/{task_id}/managed-hooks` (read; `internal/server/agent_managed_hooks.go`, through `service.ManagedHooksReader`; the slot as other agent routes take it) → `{task_id, execution_id, recorded, hooks: [{name, events, on, off: [reasons], chosen_off: [sources]}], runs: []}` for the slot's latest execution. `runs` stays empty until the runs op (LEG-699). `recorded` is false for an execution with no rows: one from before the feature, or started while the folder held none. `404` with no agent, `501` without the reader.
