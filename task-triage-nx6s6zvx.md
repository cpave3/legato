# `nx6s6zvx` — Update swarm settings

| | |
| --- | --- |
| **Type** | Change request |
| **Verdict** | Feasible. Legato already separates conductor and fallback-worker adapters, but the public YAML shape is flat and has no direct role-level model setting. |
| **Cause / Gap** | `SwarmConfig` exposes `default_agent`, `conductor_agent`, and `conductor_tier`; model selection only exists indirectly through adapter tiers and `launch_args`, so the requested `conductor.agent/model` and `worker.agent/model` profiles cannot be expressed (`config/config.go:148-186`). |
| **Recommendation** | Replace the flat adapter/tier settings with `swarm.conductor` and `swarm.worker` role profiles, propagate a direct model through the spawn/adapter contract, preserve plan-level worker overrides, and reject the removed keys with an actionable migration error. |
| **Severity / Value** | Medium value: users can select independently optimized conductor and worker harness/model combinations without defining one-off adapter tiers. |
| **Confidence** | Confirmed by tracing config load, validation, service wiring, and both launch paths. |
| **Effort** | Medium (approximately 2–4 engineering days including tests and documentation). |
| **Source** | Legato task `nx6s6zvx`; reporter/date/priority not provided. |

## What was reported

The requester wants swarm configuration to group the conductor and worker defaults independently and allow each role to select both an agent adapter and a model:

```yaml
swarm:
  conductor:
    agent: chimera
    model: manticore/sol
  worker:
    agent: chimera
    model: manticore/luna
```

The prescribed migration replaces `swarm.conductor_agent` and `swarm.default_agent`. No deadline, compatibility requirement, or explicit precedence rules for plan-level `agent`/`tier` overrides were supplied.

The underlying need is to run orchestration on a stronger model and routine worker tasks on a different model while retaining a single, readable swarm configuration.

## Current behaviour

- YAML is decoded into `config.SwarmConfig`, whose role-selection fields are `DefaultAgent`, `ConductorAgent`, and `ConductorTier` (`config/config.go:148-186`). There is no role profile or direct model field.
- A normal conductor resolves `ConductorAgent`, then falls back to `DefaultAgent`, and passes `ConductorTier` into `AgentSpawnOptions` (`internal/service/swarm.go:592-610`). This already separates the conductor adapter from the worker default.
- A worker resolves its persisted plan-level `agent` first and otherwise uses `DefaultAgent`; its persisted plan-level `tier` is passed to the spawn (`internal/service/swarm.go:978-1004`). This means heterogeneous per-subtask workers are already supported and should remain more specific than role defaults.
- `AgentSpawnOptions` carries `AgentKind` and `Tier`, but no model (`internal/service/agent.go:104-127`). `SpawnAgent` forwards only the tier to `LaunchCommand` (`internal/service/agent.go:731-738`).
- The adapter contract accepts a tier name rather than a model (`internal/service/adapter.go:31-50`). Claude Code, Chimera, and Codex resolve that tier to configured arguments, commonly `--model <value>` (`internal/engine/hooks/claude_code.go:69-98`, `internal/engine/hooks/chimera.go:41-44`, `internal/engine/hooks/codex.go:76-90`).
- Tiers are adapter-scoped named launch profiles. Plan validation resolves a worker tier against the subtask's explicit adapter or `DefaultAgent` (`internal/engine/swarm/plan.go:152-177`). The conductor tier is separately validated at process startup (`config/config.go:397-423`).
- The requested outcome is possible today only through extra tier configuration, for example defining `sol` and `luna` under `adapters.chimera.tiers`, setting `conductor_tier: sol`, and ensuring every worker selects `tier: luna`. There is no global worker tier/model default (`docs/ai/swarm.md:237-293`).
- The TUI and swarm CLI each copy the flat fields into `service.SwarmConfig` (`cmd/legato/main.go:1788-1801`, `cmd/legato/main.go:2442-2454`). The standalone server repeats similar wiring (`cmd/legato/main.go:1462-1477`).
- Learning currently treats `swarm.default_agent` as its fallback curator adapter (`config/config.go:495-508`, `cmd/legato/workflow_trigger_runtime.go:35`). A key rename therefore affects more than swarm dispatch.
- Ad-hoc swarm creation promotes an already-running session rather than launching a replacement (`internal/service/swarm.go:621-693`). Its current adapter is retained (`internal/service/swarm.go:703-710`), so a configured conductor model cannot retroactively change that process.

## Why it isn't supported

The configuration layer has enough information to choose separate adapters, but the representation is split across three flat fields and the launch boundary has no model parameter. A raw `model` cannot be forwarded by only changing YAML structs: the service can currently send a tier name, and only an adapter can turn that tier into model-specific CLI arguments (`internal/service/adapter.go:31-50`).

The current design also makes a worker-wide model default impossible. `tier` belongs to each plan subtask and is persisted with it (`internal/service/swarm.go:773-783`); dispatch has no fallback tier/model field after loading that row (`internal/service/swarm.go:986-1004`).

**Nearest analogue:** adapter tiers already carry model launch arguments end to end: plan validation resolves the adapter/tier pair (`internal/engine/swarm/plan.go:152-177`), dispatch forwards the tier (`internal/service/swarm.go:986-1004`), and adapters append the tier arguments after base arguments (`internal/engine/hooks/claude_code.go:83-98`). The new direct model should follow the same launch flow but remain distinct from tiers, because tiers are named, conductor-visible profiles that plans may override.

## Scope

- **Required — configuration contract and compatibility:** add nested `conductor`/`worker` profile structs; remove the public flat fields; return an actionable config error for `default_agent`, `conductor_agent`, or `conductor_tier` rather than allowing `yaml.v3` to ignore them and silently select defaults (`config/config.go:309-353`).
- **Required — validation:** validate non-empty configured profile adapters against the supported adapter registry and trim/reject blank model values. Replace `ValidateConductorTier` and update its startup callers (`cmd/legato/main.go:1394-1402`, `cmd/legato/main.go:1595-1604`, `cmd/legato/main.go:2355-2364`).
- **Required — service contract:** replace flat service fields with conductor/worker profile values and resolve them in `StartSwarm` and `Dispatch` (`internal/service/swarm.go:22-49`, `internal/service/swarm.go:596-610`, `internal/service/swarm.go:986-1004`).
- **Required — adapter launch contract:** add direct model propagation through `AgentSpawnOptions` and the launch adapter boundary, then have all three adapters append their supported model selector with shell quoting. Tier arguments must retain final precedence when a plan specifies a tier (`internal/service/agent.go:104-127`, `internal/service/agent.go:731-738`).
- **Required — all runtime constructors:** update TUI, standalone server, and swarm CLI service construction. Prefer a shared adapter-construction helper because adapter setup is currently duplicated (`cmd/legato/main.go:1696-1759`, `cmd/legato/main.go:2380-2431`). The standalone server currently constructs `AgentService` without the configured adapter registry (`cmd/legato/main.go:1441-1459`), so it must be brought onto the shared path for the new settings to work when a swarm starts through `legato serve`.
- **Required — plan precedence:** retain explicit subtask `agent` and `tier`. Recommended resolution is: explicit subtask agent overrides `worker.agent`; explicit tier overrides `worker.model`; if an explicit agent differs from `worker.agent` and no tier is supplied, do not apply the worker model because model identifiers are adapter-specific (`internal/engine/swarm/plan.go:152-177`).
- **Required — dependent learning fallback:** replace `swarm.default_agent` references with `swarm.worker.agent` in learning validation and runtime defaults (`config/config.go:495-508`, `cmd/legato/learning_cmd.go:366`, `cmd/legato/workflow_trigger_runtime.go:35`).
- **Required — docs:** update the configuration reference and swarm examples/precedence explanation (`docs/ai/config.md:70-83`, `docs/ai/swarm.md:156-293`, `docs/ai/swarm.md:395-428`) plus learning references to `swarm.default_agent` (`docs/ai/learning.md:121-136`).
- **Not affected — persistence/migrations:** role defaults are process configuration. Existing subtask `agent_kind` and `tier` persistence remains sufficient; no database migration or backfill is needed (`internal/service/swarm.go:773-783`).
- **Not affected — API/UI/permissions:** start-swarm API, TUI, and web inputs do not expose adapter/model selection; they consume the configured service. No authorization, mobile, accessibility, localization, tenancy, or timezone change is implied.
- **Not affected — performance:** launch-time string/config resolution is constant-time and outside hot data paths.
- **Maybe — observability:** include effective adapter/model in launch logs or durable execution metadata only if operators need auditability. This is useful but not required by the ticket.
- **Rollout:** config-only breaking change. No feature flag is warranted; rollback is binary plus config rollback.

## Minimal viable slice

Implement exactly the nested profiles, direct model propagation, documented precedence, migration errors for old keys, and end-to-end tests for newly spawned conductors/workers. Keep per-subtask `agent` and `tier`, adapter-level `launch_args`/tiers, and all UI/API shapes unchanged.

This leaves out model selection in the swarm plan, runtime model switching, model validation against a provider catalog, and changing the model of an ad-hoc promoted conductor.

## Impact and value

Users who routinely run mixed-cost swarms gain one stable default for orchestration and another for fan-out work. Today they must model this through adapter tiers and rely on the conductor to emit a tier on every worker, which is more verbose and can launch the adapter's base model when omitted (`docs/ai/swarm.md:271-293`).

Existing users must migrate the renamed flat keys. Users who omit all role settings retain the existing built-in adapter fallback, provided the implementation preserves the current default-adapter behavior (`docs/ai/swarm.md:229-235`).

## Ruled out

- ~~Only rename `default_agent` and `conductor_agent`~~ — insufficient because neither `service.SwarmConfig` nor `AgentSpawnOptions` can carry a raw model (`internal/service/swarm.go:22-49`, `internal/service/agent.go:104-127`).
- ~~Represent `model` as an implicit synthetic tier~~ — tiers are validated named profiles, shown to the conductor, and persisted per subtask (`internal/engine/swarm/plan.go:152-177`, `internal/service/tier_catalog.go:8-25`). Synthesizing hidden tiers would blur two distinct precedence rules and risk name collisions.
- ~~Use adapter-level `launch_args` only~~ — those arguments apply to every participant using that adapter, so they cannot select different models when both roles use Chimera (`config/config.go:112-136`).
- ~~Remove plan-level `agent` and `tier`~~ — those are established heterogeneous-worker extension points and are more specific than role defaults (`docs/ai/swarm.md:206-220`, `docs/ai/swarm.md:277-291`).
- ~~Silently accept both old and new keys indefinitely~~ — this creates two sources of truth and ambiguous precedence. It also leaves the migration unfinished; a targeted error is safer than silently ignoring removed YAML keys.

## Options

### Option 1 — Nested role profiles with direct model support (recommended)

Add a shared profile type such as `SwarmParticipantConfig { Agent, Model string }`, use it for `swarm.conductor` and `swarm.worker`, and carry `Model` through spawn and adapter launch. Preserve plan-level overrides with explicit precedence. Reject legacy keys with a migration message.

**Effort:** medium. **Risk:** medium, concentrated in duplicated runtime wiring and precedence. **Does not deliver:** provider-side model discovery/validation or per-subtask raw model selection.

### Option 2 — Nested profiles backed by named tiers

Expose `conductor.agent/tier` and `worker.agent/tier`, requiring users to define model launch arguments under `adapters.<agent>.tiers`. This reuses the complete existing tier pipeline and is lower risk.

**Effort:** small to medium. **Risk:** low. **Does not deliver:** the requested `model: manticore/sol` syntax; users still need tier boilerplate, so it only partially satisfies the task.

### Option 3 — Document the current tier workaround

Keep the schema and show users how to configure conductor and per-worker tiers.

**Effort:** very small. **Risk:** very low. **Does not deliver:** a worker-wide default model or the requested nested settings, and omissions can still use the base model.

## Recommendation

Choose Option 1. It matches the requested contract, keeps the existing flexible plan system, and makes role defaults explicit. Option 2 is technically simpler but does not provide the requested direct model setting; Option 3 leaves the underlying usability gap intact.

Recommended precedence:

1. **Conductor:** `swarm.conductor.agent/model`; an empty agent uses the registered default, and an empty model uses the adapter's normal default.
2. **Worker adapter:** plan subtask `agent` → `swarm.worker.agent` → registered default.
3. **Worker model/profile:** plan subtask `tier` wins over `swarm.worker.model`. A changed explicit subtask adapter should not inherit a model coupled to a different `worker.agent`.
4. **Adapter arguments:** base `launch_args` first, role model selector second, explicit tier arguments last, preserving the current guarantee that a tier wins (`config/config.go:131-145`).

**Files to touch**

- `config/config.go` — define nested profiles, validate adapters/models, reject legacy keys, replace conductor-tier validation, and move learning fallback to `worker.agent`.
- `config/config_test.go` — parse the exact requested YAML; test empty/default profiles, invalid adapters, and actionable errors for each removed key.
- `config/learning_test.go` — verify curator fallback uses `swarm.worker.agent`.
- `internal/service/agent.go` — add model to spawn options and pass it to adapter launch.
- `internal/service/adapter.go` — extend the launch contract (or add a focused model-capable interface) with explicit model/tier precedence.
- `internal/engine/hooks/claude_code.go`, `internal/engine/hooks/chimera.go`, `internal/engine/hooks/codex.go` — safely append the adapter's model selector before tier args.
- Corresponding hook tests — assert model shell quoting, empty-model behavior, and tier-over-model ordering.
- `internal/service/swarm.go` — consume conductor/worker profiles in `StartSwarm` and `Dispatch`.
- `internal/service/swarm_test.go` — assert effective adapter and model for the conductor and fallback worker, plus explicit worker agent/tier precedence.
- `cmd/legato/main.go` — update all three swarm constructors and consolidate configured adapter creation so TUI, server, and CLI agree.
- `cmd/legato/learning_cmd.go`, `cmd/legato/workflow_trigger_runtime.go`, and their tests — update the learning fallback.
- `docs/ai/config.md`, `docs/ai/swarm.md`, `docs/ai/learning.md` — document the new schema, migration, and precedence.

**Verification**

1. Load the ticket's exact YAML and assert conductor `{agent: chimera, model: manticore/sol}` and worker `{agent: chimera, model: manticore/luna}`.
2. Through `SwarmService.StartSwarm`, capture the real adapter launch command and assert the conductor receives `--model manticore/sol`.
3. Apply and dispatch a plan subtask with no `agent` or `tier`; assert Chimera launches with `--model manticore/luna`.
4. Dispatch a subtask with an explicit different `agent`; assert that adapter is used and the Chimera worker model is not leaked.
5. Dispatch a subtask with a tier; assert tier launch arguments occur after and override the worker model.
6. Exercise TUI/server/CLI construction tests or a shared constructor test to prove all entry points use identical profile wiring. Browser/E2E testing is unnecessary.
7. Run `go test ./config ./internal/engine/hooks ./internal/service ./cmd/legato` and then the repository's standard `task test`/`task build` checks.

**Rollout concerns**

No database migration or backfill. Release notes must include a mechanical YAML migration:

```yaml
# before
swarm:
  conductor_agent: chimera
  default_agent: chimera
  conductor_tier: sol

# after
swarm:
  conductor:
    agent: chimera
    model: manticore/sol
  worker:
    agent: chimera
    model: manticore/luna
```

`conductor_tier` is not semantically identical to `conductor.model`; users with complex tier launch arguments must move the model to the profile and retain any non-model behavior in adapter `launch_args`. Legacy keys should fail startup with this guidance rather than silently changing behavior.

**Related decisions**

- Commit `dd03766` introduced `conductor_agent`, conductor tiers, and the current backwards-compatible flat fallback. This request intentionally supersedes that public shape while retaining its split-role behavior.
- No relevant ADR or prior rejection was found.
- Peer task `ris6nnxz` is modifying reflection command/schema files and `docs/ai/learning.md`; coordinate the documentation hunk if both changes are implemented concurrently. It does not overlap the core config/swarm files.

## Risks and unknowns

- The ticket does not define whether a direct model is adapter-agnostic. The recommendation treats it as an adapter-owned launch value and avoids applying a profile model after a subtask switches adapters.
- Model identifiers are not currently discoverable or validated. Legato can validate the adapter name but should let the external agent CLI report an invalid model, unless model catalogs are added as a separate feature.
- Changing the existing `LaunchCommandAdapter` signature touches adapters and test fakes. A new optional capability can reduce interface churn, but it must still produce one correctly ordered command; avoid parallel launch paths.
- `legato serve` currently does not configure the adapter registry in the same way as TUI/CLI (`cmd/legato/main.go:1441-1459`). Centralizing construction is recommended so this task is genuinely end-to-end rather than preserving that divergence.
- Ad-hoc promotion cannot honor `conductor.model` without restarting or invoking an adapter-specific live model API; retaining the current session should be documented as intentional (`internal/service/swarm.go:621-748`).

## Open questions for the reporter

1. Should the old flat keys produce a hard migration error (recommended), or is one release of compatibility required?
2. When a plan explicitly changes a worker's `agent`, should `worker.model` be cleared (recommended) or passed to the new adapter?
3. Should `conductor_tier` be removed in favor of direct `conductor.model`, or should nested profiles also retain an optional `tier` for non-model launch profiles?
