# Chimera native integration

Legato loads its embedded Chimera v1 mod by default (`adapters.chimera.native_bridge`). It connects the main session without lockfile discovery, saves its actual conversation ID for resume, reports activity and background jobs, and bundles Legato's MCP server. With MCP enabled, every spawn checks `chimera version --json` for `modApi: [1]` and fails before starting a terminal if unsupported, rather than silently losing tools. The error asks the operator to upgrade Chimera or set `adapters.chimera.native_bridge: false` for legacy MCP injection. Disabling mods with launch arguments is also refused when bundled tools are required. This capability check is not cached across binary updates; binding/state capability is still decided by the first mod event.

## Lifecycle

`configuredAdapters` installs `internal/engine/hooks/chimeramod/legato` under `<data_dir>/chimera-mod/legato`. The installer leaves unchanged files alone and excludes tests. `ChimeraAdapter.EnvVars` appends the folder to `CHIMERA_MOD_DIRS` and sets `LEGATO_BIN`, so a Chimera started by hand in the pane loads it too. Setting `native_bridge: false` leaves the old launch path in place.

The module's `register(on)` observes ordered v1 events. It does nothing without `LEGATO_TASK_ID` or when `$.session.isSubagent`. In the main session it starts one `legato agent link chimera` child with `$.process.spawn`; NDJSON on its stdin carries an increasing `seq`. The first `session.start` is enriched with `$.server.url()` (the full secret-path base URL), `$.chimera.version`, `$.api.version` and `$.session.cwd`. No interception occurs: model settings, messages, permissions and questions still use Chimera's existing HTTP protocol.

`RunChimeraLink` chooses the same store or broker `BridgeHost` as the Claude Code bridge. It validates a numeric loopback HTTP URL, binds the exact running session, and saves the actual Chimera conversation identity. `session.end` with reason `switch` keeps the child alive for the next `session.start`; a final end or stdin EOF releases only the binding it owns. A switched or subagent session cannot clear a replacement's binding. Repeated and older sequence numbers are ignored. A short broker outage pauses consumption and retries the current event rather than running later state ahead of it; a refusal is an error on stderr.

State travels through `ParseChimeraHook` / `ApplyChimeraHook`, via broker `agent.hook` with `kind: chimera` when sandboxed. The link counts open permission and question requests; resolving one leaves `waiting` until the last is resolved. See [Agent states](agent-states.md) for mappings and evidence. `hook_source = 'mod'` prevents old scripts, Claude-shaped hook reports and terminal prompt detection from overriding the mod. SQL guards return `ErrAgentStateIgnored` when a report loses authority; callers then skip reporting spans, notifications and broadcasts.

## Launch messages and fallback

A fresh session keeps `--prompt` as turn one. The adapter is a `NativeBindingAdapter` while the mod is configured: later launch messages (including a resumed reviewer's brief and peer notices) wait for the native binding and go over HTTP, rather than being typed into a TUI that is still starting.

If the mod has not bound after ten seconds, Legato begins legacy lockfile discovery. Discovery stops once either path binds and cannot replace a binding that arrived during its HTTP probe. Old Chimera builds keep existing hook reports and the manual **Upgrade to native panel** path with the mod disabled (or MCP tools disabled). Discovery cannot add MCP flags to a process already running: the MCP launch preflight refuses unsupported hosts and asks for an upgrade or `native_bridge: false`. Native endpoint recovery on Legato startup keeps a live binding, rediscovers a missing one, and respects an intentional downgrade to tmux.

## Server authentication

Each Chimera launch gets a random secret stored privately with the agent session, outside its public listing. A mode-0600 bootstrap file in the private prompt folder holds it. The launch command reads that file into `CHIMERA_SERVER_SECRET` and unlinks it before executing Chimera; neither tmux's environment nor the typed command contains the value. `SandboxLaunchCommandAdapter` places this host-only preamble before bwrap, not inside it. The bootstrap directory is masked by an empty mount even during clone setup, so a prompt-folder mount cannot disclose a pending secret. Binding cleanup is idempotent and clears the stored file path; authentication remains available for fallback discovery and restart recovery. The endpoint includes the secret as a path component, so the existing client needs no authentication changes. Treat endpoint URLs as credentials: do not log or expose them to agents.

The sandbox mounts the installed mod and `~/.chimera/mods` and `~/.chimera/hooks` read-only. Other Chimera session state stays writable. The mod's link and MCP server use the host broker, without exposing Legato's database or configuration. See [Sandbox](sandbox.md).

Known limit: Chimera's own commands can still read `CHIMERA_SERVER_SECRET` from `/proc/<pid>/environ`, including inside Legato's bwrap PID namespace. CHIM-86 tracks passing the secret by file descriptor or pipe instead; the current environment handoff does not protect the credential from those commands.

## Tools

With `agents.mcp.enabled`, the mod manifest bundles `legato mcp serve` using the concrete binary path from `LEGATO_BIN`, its trusted tool names and an explicit Legato environment map. A tools-disabled installation has no `mcpServers`. A mod launch neither probes `chimera --help` nor adds `--mcp-config`; an operator's own flag remains untouched. MCP server instructions supply the tool guidance instead of the launch prompt. With the mod disabled, the legacy flag/probe/guidance path remains. See [Agent tools](agent-mcp.md).

## Testing and evidence

The public seam is `RunChimeraLink` consuming NDJSON against a real temporary SQLite database, both directly and through the real broker. Installer, adapter and sandbox-plan tests cover their existing public seams. Native harness tests in the mod's `tests` folder export `async test(h)` and use the host's fake `$`, `emit` and recorded calls; run them with `chimera mod test internal/engine/hooks/chimeramod/legato` on a mod-capable Chimera. Separate queue/failure mock tests run with `node --test internal/engine/hooks/chimeramod/mock.test.mjs`. Neither test suite is installed.

The host is pinned to Chimera `859884e7a69943df79124bed58f87c9f6f89a99f` (pushed clean branch, Bun 1.4.4, `v0.0.0-local`; SHA-256 `c1586c10f13ba850d76870262098ae7bd036c547e6fcc72e8aad43186144cbd7`). Five native harness tests and twelve Node mock tests pass. Sixteen real TUI streams, recorded with private tmux/home/runtime folders and a local scripted provider, replay through SQLite and the real broker (32 cases). Only paths/URLs are normalized; malformed, overlapping and extreme conditions remain explicitly synthetic tests.

`internal/cli/testdata/chimera-mod/end_to_end.py` also passed with an isolated scratch Legato: API spawn, mod/link binding, actual MCP `task_context`, saved identity, anonymous HTTP 404/authenticated 200, no secret in argv/tmux, consumed bootstrap, scratch-server restart recovery, a later message and `/exit` cleanup. Its agent is unsandboxed; sandbox protection is checked at the plan/spawn and executable-wrapper seams. No existing Legato instance was tested or restarted.

The [fixture evidence matrix](../../internal/cli/testdata/chimera-mod/README.md) gives provenance and reproduction commands. LEG-179's exact 18-flake-point audit remains pending because its withdrawn plan was not available on this worker. A separate pre-existing question-protocol mismatch remains: Legato's native-panel answer transport and question IDs do not match current Chimera; question-state recordings resolve through Chimera's canonical endpoint, not that UI path. Do not treat those recordings as proof of native-panel answering.
