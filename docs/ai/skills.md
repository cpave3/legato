# Skills

Legato keeps a folder of skills and gives the ones that are on to every agent it starts, whichever tool runs it and wherever it runs (LEG-216, milestone LEG-215). Site page: `agent-tools/skills`.

v1 is deliberately small: skills are folders on disk, Settings lists them and turns each on or off, and nothing in the UI adds, edits or removes one. Per-repository, per-role or per-kind targeting, built-in skills, the Reeve's own skills and adding skills from the UI are LEG-137. Profiles (named sets of skills and MCP tools) come later; see § Designed for more.

## The skills folder

- **Where**: `skills.dir` (`config.SkillsConfig`, `config/skills.go`), else `<legato home>/skills`: `~/.legato/skills`, or `$LEGATO_HOME/skills` (`swarm.LegatoHome`). `ResolveSkillsDir(cfg, legatoHome)`; `cmd/legato/skills_runtime.go` (`skillsDir`). A set value must be absolute or start with `~/` (`normalizeSkills`). Registered as a `risky` string with mode `restart`: the services are built with the folder, and the folder decides which files every agent is given.
- **A skill** is a folder in it with a `SKILL.md` whose YAML frontmatter has `name` and `description` (`internal/engine/skills`, `Scan`). A skill linked into the folder (a registry checkout) counts; files at the top and folders starting with `.` are not listed. A folder that is not a usable skill is listed with its error (`Skill.Err`), never dropped and never a failure of the scan:
  - the folder's name is not lowercase letters, digits and hyphens of at most 64 characters (Claude Code's and the Agent Skills format's rule);
  - no `SKILL.md`; no frontmatter, or no closing `---`; frontmatter that is not YAML;
  - no `name`, or a `name` other than the folder's (Chimera skips such a skill, and Claude Code calls a plugin's skill by its folder);
  - no `description` (Chimera skips it; an agent decides when to use a skill by it).
- **Finding changes**: a rescan, not a watch. `SkillService.List` scans the folder at every ask, and so does every spawn, so a folder added, changed or removed shows at the next list and reaches the next agent without a restart. Settings asks every 10 s and when the window gets focus. Scanning reads one `SKILL.md` per folder; there is no fsnotify in the module.
- **Legato never writes into the skills folder.**

## On and off

`skill_states` (migration 116; `name` the folder name, `enabled`, `updated_at`), `store.SkillStates` and `SetSkillEnabled`. A skill with no row is **on**: putting its folder in the skills folder is how the operator adds a skill in v1, so a second step to turn it on would only be a way to forget one; turning it off is the opt-out. A row outlives its folder, so a skill removed and put back keeps its state. `SkillService.SetEnabled` refuses a name that is not a folder of the skills folder (`ErrSkillNotFound`), errored skills included; a skill with an error is never given, whatever its state.

## API and Settings

- `GET /api/settings/skills` (read) → `{dir, skills: [{name, description, path, error?, enabled}]}`, sorted by name.
- `PUT /api/settings/skills/{name}` (change) `{enabled: bool}` → the skill. `400` for a body without `enabled`, `404` for no such folder, `503` without the service (`internal/server/skills.go`). Being a `change` route, the Reeve turns a skill on or off through a change set.
- **Settings › Agents › Skills** (`web/src/components/settings/SkillsSettings.tsx`, `#skills`, `web/src/lib/skills.ts`): the folder, then a row per skill with its name, description (or its error, marked **error**), path, and a toggle (**Give agents <name>**; none for a skill with an error). With none: "No skills yet. Add a folder with a SKILL.md to <dir>."
- The TUI has no settings view, so no list there.

## Giving skills to an agent

At each spawn, after the session's environment is built and before the sandbox is planned, `agentService.giveCapabilities` (`internal/service/agent_capabilities.go`):

1. asks the `CapabilitySource` (`AgentServiceOptions.Capabilities`, the `SkillService`) for the agent's `AgentCapabilities`: every skill that is on and has no error. It asks for every agent whose tool has an adapter, whatever kinds the adapter takes;
2. if there are any skills and the agent's adapter is a `SkillsAdapter`, has it write its bundle in `<data_dir>/capabilities/<execution>/<adapter name>/` (`CapabilityFolders`, `AgentServiceOptions.CapabilityFolders`) and add to the session's environment what makes its tool load it;
3. copies each skill into the bundle's `skills/<name>/` (`skills.Copy`: only what is inside the skill's folder, through an `os.Root`; a link to a file inside is copied as the file, a link out of the folder, to a folder, or nowhere is left out, so a skill cannot carry a host file such as a key into a sandbox; executable bits kept; a skill of more than 32 MiB is refused and logged).

A failure is logged and the agent starts without what failed: a skill is an addition, never a reason not to start. The environment is changed only once the bundle is written.

### Capabilities given per kind

Each kind of capability is given on its own (`capabilityKinds.give`), skills first, then managed hooks:

- **Its own adapter check.** Skills need a `SkillsAdapter`, managed hooks a `HooksAdapter`; a kind with nothing chosen is skipped. Hooks take their branch when there are no skills.
- **Its own folder** inside the execution's tool folder: `legato-skills/` and (from LEG-696) `legato-hooks/`, which the adapter names.
- **Its own environment change**, made on a copy of the session's environment and committed only once its bundle is written. The second kind starts from the environment the first committed, so both append to `CLAUDE_CODE_PLUGIN_DIRS`.
- **Its own failure.** A bundle that fails is logged and what it added to the tool folder is removed (the entries that were not there before it ran); then the tool and execution folders go if that leaves them empty. A kind that failed never takes another from the agent, and the agent starts.

With nothing chosen, nothing is written and the environment is unchanged.

### The capability request

`CapabilityRequest` is what the spawn knows of the agent; a field it does not know is empty:

| Field | From |
| --- | --- |
| `TaskID`, `ExecutionID`, `AgentKind`, `Role`, `Slot`, `WorkDir` | the spawn |
| `Repository` | the repository the agent works in, `host/owner/name` as `config.NormalizeRepository` writes it: the key repository resources and guidance use (`spawnProjectKey`; "" for a folder that names no repository) |
| `WorkflowRunID`, `WorkflowAttemptID`, `WorkflowStepID` | the workflow step attempt that started it: `AgentSpawnOptions.WorkflowRunID`/`WorkflowAttemptID`, else the session's `LEGATO_WORKFLOW_*` identity (a resumed workflow agent names only its run), and the step from that identity (`spawnWorkflowStep`) |
| `ReadOnly` | `AgentSpawnOptions.ReadOnlyCheckout` (a reviewer, a find-out delegation) |
| `Reeve` | the task is a Reeve session's (`IsReeveTask`) |
| `HandoffPurpose` | `AgentSpawnOptions.HandoffPurpose`, which the handoff worker sets to the handoff's purpose (`make-change`, `find-out`); "" otherwise, and not carried by a restart |

### Managed hooks: the seam

`AgentCapabilities.Hooks` (`[]ManagedHook`: `Name`, `Path`, `Events`) is the place for an agent's managed hooks (milestone LEG-687). No source fills it yet: the `HookSource` (LEG-695) will. An adapter takes them by implementing `HooksAdapter`:

```go
HooksBundle(root string, events []string, env map[string]string) (hooksDir string, err error)
```

It writes its bundle under `root` (the execution's tool folder), registering each event in `events` (every event the hooks use, once each, sorted), adds to `env` what makes its tool load it, and returns the folder each hook's folder is copied into (with `skills.Copy`, as a skill is). A tool is a supported tool for managed hooks exactly when its adapter implements `HooksAdapter`; no adapter does yet (Claude Code will from LEG-696), so the policy is tested against a fake adapter (`agent_capability_kinds_test.go`). Each execution has its own copy, so turning a skill off, or changing its files, never changes a running agent; the next agent started, or an agent reloaded (a new execution), gets the skills as they are then. `EndExecution` removes the folder; `ReconcileSessions` sweeps the folders of executions that ended unseen (`CapabilityFolders.Sweep`, as `SandboxScratch.Sweep`: running and unknown executions are left).

| Tool | How | What the agent calls it | Same name elsewhere |
| --- | --- | --- | --- |
| Claude Code | A plugin `legato-skills` (`.claude-plugin/plugin.json`, `skills/`) appended to `CLAUDE_CODE_PLUGIN_DIRS` (`ClaudeCodeAdapter.SkillsBundle`, `internal/engine/hooks/skills_bundle.go`) | `legato-skills:<name>` (`/legato-skills:<name>`) | Both are offered: a project's or the person's own `<name>` and `legato-skills:<name>` (checked with Claude Code 2.1.325) |
| Chimera | A mod `legato-skills` (`chimera-mod.json`: `name`, `version`, `apiVersion: 1`, `skills: "skills"`, no `module`) appended to `CHIMERA_MOD_DIRS`, which Chimera loads without enabling it and its subagents inherit (`ChimeraAdapter.SkillsBundle`) | `<name>`, source `mod:legato-skills` | First wins in Chimera's order: the project's `.chimera/skills` (the working directory, then up to the git root), then `~/.chimera/skills`, then mods (`--mod`, then `CHIMERA_MOD_DIRS` in order, then `~/.chimera/mods`), then `.claude/skills` (cwd, ancestors, home), `.codex/skills` and `.agents/skills` when their compatibility is on, then Chimera's built-ins. So the person's or the project's `.chimera` skill beats Legato's, and Legato's beats a `.claude` one and the built-ins; the loser is recorded as a collision |
| Codex | Not supported in v1: Codex reads skills from `$CODEX_HOME/skills` only, and moving `CODEX_HOME` moves its login and sessions. Writing into `~/.codex/skills` would give the skills to every Codex session, not one execution's | – | – |

Chimera's facts are from its source at 5bb563c (`packages/mods/src/index.ts`, `packages/skills/src/discover.ts`, `packages/core/src/discovery.ts`).

## The sandbox

- The execution's folder (`SandboxPlanInput.CapabilitiesDir`, set by `sandboxPrefix` when it exists) is mounted read-only at its own path, and allowed inside the data folder (`allowedInside`). The data folder itself stays hidden, so no execution sees another's. Claude Code loads a plugin from a read-only folder (checked under bwrap).
- The skills folder (`SandboxPlanInput.SkillsDir`, from `skillsDir` in `sandboxSettings`) is a forbidden path: never mounted, refused as a working directory or a mount inside it, and covered when a wider mount (a resource on `~/.legato`) would show it. An agent works with its copy and cannot change the skills later agents get.

## Designed for more

The seams in `agent_capabilities.go` are meant to be reused, not rebuilt:

- **Profiles**: a profile is another `CapabilitySource`, choosing from the `CapabilityRequest` (task, execution, agent kind, role, slot, working directory, repository, workflow step, read-only, Reeve, handoff purpose) which skills an agent gets. `SkillService.Capabilities` gives all that are on; nothing else changes.
- **MCP broker tools (LEG-212)**: `AgentCapabilities` gains a field beside `Skills`; a source fills it; the execution's capability folder is the place for any per-execution file a tool needs to find them (a plugin's `.mcp.json`, a mod's `mcpServers`), which the sandbox already shows read-only and which goes with the execution; an adapter interface beside `SkillsAdapter` writes it, as `HooksAdapter` does for managed hooks, and `giveCapabilities` gives it as a kind of its own (§ Capabilities given per kind). Adding a tool to a session at its start, and nothing while it runs, is the same contract LEG-199's analysis asks for.
