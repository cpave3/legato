# Managed hooks

Legato keeps a folder of managed hooks: hooks the operator writes once, which Legato gives to the agents it starts, translated for each agent tool, on unless something turns them off (spec LEG-687; Phase 1 milestone tickets LEG-693 to LEG-704). The first is CodeEval's net-debt gate at the end of each turn. Site page: `agent-tools/managed-hooks`.

A managed hook is never a repository's own hook (`.claude/settings.json`, `.chimera/hooks/`) nor one in the operator's agent-tool settings (`~/.claude/settings.json`): those reach every session of their tool, and Legato neither reads nor changes them here.

## Phase 1 scope

Phase 1 (this milestone) delivers managed hooks to Claude Code, with a global off switch. What exists so far (LEG-694):

- the setting `managed_hooks.dir` and the folder;
- the scan and full validation of `HOOK.yaml` (`internal/engine/managedhooks`), including what Phase 1 does not act on yet, so a manifest valid today stays valid later;
- the global on/off state (`managed_hook_states`);
- Settings › Agents › Managed hooks and its routes;
- the policy (LEG-695): every spawn decides and records which hooks its agent has and why each other is off, takes `hooks_off`, and notes a tool that cannot run them (§ The policy).

Nothing reaches an agent tool yet, since no adapter is a `HooksAdapter` until the Claude Code bundle (LEG-696). Until then every agent records its hooks `off:unsupported-tool`, with a skip note. Next come resources and the sandbox (LEG-697), resources and the sandbox (LEG-697), the runner (LEG-698), the runs op (LEG-699), the stop hold (LEG-700), warnings (LEG-701), spawn offs (LEG-702) and web visibility (LEG-703) follow. Chimera, `pre_tool`/`post_tool` delivery, repository offs, workflows and handoff are Phases 2 and 3 (LEG-690, LEG-691).

## Phase 1 limits

Kept current by each Phase 1 ticket; the site page's section of the same name says the same.

- **Claude Code only.** Chimera and Codex agents get no managed hooks (`off:unsupported-tool`), with one skip note per execution.
- **Workflow agents get none** (`off:workflow`): any execution with a workflow attempt, until Phase 3's step-complete skip lands, when the reason is removed.
- **`pre_tool` and `post_tool` are not delivered yet.** They are validated, listed and shown, but only `session_start` and `stop` run.
- **No repository offs and no `yields_to`.** A repository that runs CodeEval's own hook (the code-evaluator repository) runs it twice; untick the hook at spawn.
- **`disableAllHooks` silently turns the gate off,** with no warning.
- **No never-ran warning.**

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

- `GET /api/settings/managed-hooks` (read) → `{dir, hooks: [{name, description, events, agent_tools, read_only, path, error?, enabled}], repositories: []}`, sorted by name. `repositories` stays empty until repository offs (Phase 3).
- `PUT /api/settings/managed-hooks/{name}` (change) `{enabled: bool}` → the hook. `400` for a body without `enabled` or not JSON, `404` for no usable hook of that name (absent or errored), `503` without the service (`internal/server/managed_hooks.go`). Being a `change` route, the Reeve turns a hook on or off through a change set.
- **Settings › Agents › Managed hooks** (`web/src/components/settings/ManagedHooksSettings.tsx`, `#managed-hooks`, `web/src/lib/managedHooks.ts`): the folder, then a row per hook with its name, description, events, agent tools ("every agent tool" when empty, "read-only agents too" when `read_only`) and path, or its error marked **error**; a toggle **Give agents <name>** on valid hooks only. With none: "No managed hooks yet. Add a folder with a HOOK.yaml to <dir>." The registry row `managed_hooks.dir` sits in the same group. The list, its refresh and its calls are shared with Skills (`useFolderToggles`, `FolderItemRow`, `web/src/lib/folderItems.ts`).
- `cmd/legato/managed_hooks_runtime.go` builds `managedHookService(cfg, db)` for both web servers (`SetManagedHookService`).

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
- `ManagedHookSet.On()` is what `AgentCapabilities.Hooks` gets (`ManagedHook`: `Name`, `Path`, `Events`, and `Manifest`, the scanned `managedhooks.Hook` with handlers, resources and `yields_to`, for the bundle); `giveCapabilities` gives them through the `HooksAdapter`, a kind of their own beside skills.

**Records.** `agentService.decideManagedHooks`, run by `giveCapabilities` at each spawn once the execution exists, writes every hook's decision to `execution_managed_hooks` and each chosen off to `execution_managed_hooks_off` (see [Database](database.md) § Execution managed hooks). Only the chosen offs are a relaunch's to carry (LEG-702); the other layers are worked out again at each spawn. A hook turned off later leaves a running agent's record alone. `GET /api/agents/{task_id}/managed-hooks` reads the slot's newest session's execution, running or not, and once a stop has deleted that session, the slot's newest execution whose hooks a spawn decided, hooks or none (`execution_managed_hook_owners`), so a stopped agent's records stay readable and an older execution is never shown in place of a newer one with no hooks. Without a `HookSource`, or when it fails (logged), nothing is recorded and the agent gets no hooks.

**Notes** (task notes, source `legato`, one each per execution at most):

- a `hooks_off` name that is no hook in the folder: "hooks_off named no managed hook, so it was ignored: <names>." Nothing is recorded for it.
- the skip note, when the tool cannot run managed hooks and at least one hook would otherwise apply (`ManagedHookDecision.ToolSkipped`: off for no reason but the tool): "Managed hooks skipped for <tool>, which cannot run them yet: <names>. The agent started without them." No folder, or only hooks off for other reasons, writes none. There is no skills skip note, so Phase 1 never writes "skills and managed hooks skipped".

**Route.** `GET /api/agents/{task_id}/managed-hooks` (read; `internal/server/agent_managed_hooks.go`, through `service.ManagedHooksReader`; the slot as other agent routes take it) → `{task_id, execution_id, recorded, hooks: [{name, events, on, off: [reasons], chosen_off: [sources]}], runs: []}` for the slot's latest execution. `runs` stays empty until the runs op (LEG-699). `recorded` is false for an execution with no rows: one from before the feature, or started while the folder held none. `404` with no agent, `501` without the reader.
