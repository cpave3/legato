# Chimera native integration

Legato loads its embedded Chimera v1 mod by default (`adapters.chimera.native_bridge`). It connects the main session without lockfile discovery, saves its actual conversation ID for resume, reports activity and background jobs, and bundles Legato's MCP server. This requires a Chimera that reports `modApi: [1]`; older builds ignore the mod directory and use lockfile discovery instead.

## Lifecycle

`configuredAdapters` installs `internal/engine/hooks/chimeramod/legato` under `<data_dir>/chimera-mod/legato`. The installer leaves unchanged files alone and excludes tests. `ChimeraAdapter.EnvVars` appends the folder to `CHIMERA_MOD_DIRS` and sets `LEGATO_BIN`, so a Chimera started by hand in the pane loads it too. Setting `native_bridge: false` leaves the old launch path in place.

The module's `register(on)` observes ordered v1 events. It does nothing without `LEGATO_TASK_ID` or when `$.session.isSubagent`. In the main session it starts one `legato agent link chimera` child with `$.process.spawn`; NDJSON on its stdin carries an increasing `seq`. The first `session.start` is enriched with `$.server.url()` (the full secret-path base URL), `$.chimera.version`, `$.api.version` and `$.session.cwd`. No interception occurs: model settings, messages, permissions and questions still use Chimera's existing HTTP protocol.

`RunChimeraLink` chooses the same store or broker `BridgeHost` as the Claude Code bridge. It validates a numeric loopback HTTP URL, binds the exact running session, and saves the actual Chimera conversation identity. `session.end` with reason `switch` keeps the child alive for the next `session.start`; a final end or stdin EOF releases only the binding it owns. A switched or subagent session cannot clear a replacement's binding. Repeated and older sequence numbers are ignored. A short broker outage pauses consumption and retries the current event rather than running later state ahead of it; a refusal is an error on stderr.

State travels through `ParseChimeraHook` / `ApplyChimeraHook`, via broker `agent.hook` with `kind: chimera` when sandboxed. The link counts open permission and question requests; resolving one leaves `waiting` until the last is resolved. See [Agent states](agent-states.md) for mappings and evidence. `hook_source = 'mod'` prevents old scripts and terminal prompt detection from overriding the mod.

## Launch messages and fallback

A fresh session keeps `--prompt` as turn one. The adapter is a `NativeBindingAdapter` while the mod is configured: later launch messages (including a resumed reviewer's brief and peer notices) wait for the native binding and go over HTTP, rather than being typed into a TUI that is still starting.

If the mod has not bound after ten seconds, Legato begins legacy lockfile discovery. Discovery stops once either path binds and cannot replace a binding that arrived during its HTTP probe. Old Chimera builds keep the existing hook reports and manual **Upgrade to native panel** path. Native endpoint recovery on Legato startup keeps a live binding, rediscovers a missing one, and respects an intentional downgrade to tmux.

## Server authentication

Each Chimera launch gets a random secret stored privately with the agent session, outside its public listing. A mode-0600 bootstrap file in the private prompt folder holds it. The launch command reads that file into `CHIMERA_SERVER_SECRET`; neither tmux's environment nor the typed command contains the value. Binding removes the bootstrap file, while the stored value remains available for authenticated fallback discovery and restart recovery. The endpoint includes the secret as a path component, so the existing client needs no authentication changes. Treat endpoint URLs as credentials: do not log or expose them to agents.

The sandbox mounts the installed mod and `~/.chimera/mods` and `~/.chimera/hooks` read-only. Other Chimera session state stays writable. The mod's link and MCP server use the host broker, without exposing Legato's database or configuration. See [Sandbox](sandbox.md).

## Tools

With `agents.mcp.enabled`, the mod manifest bundles `${LEGATO_BIN} mcp serve`, its trusted tool names and an explicit Legato environment map. A tools-disabled installation has no `mcpServers`. A mod launch neither probes `chimera --help` nor adds `--mcp-config`; an operator's own flag remains untouched. MCP server instructions supply the tool guidance instead of the launch prompt. With the mod disabled, the legacy flag/probe/guidance path remains. See [Agent tools](agent-mcp.md).

## Testing and evidence

The public seam is `RunChimeraLink` consuming NDJSON against a real temporary SQLite database, both directly and through the real broker. Installer, adapter and sandbox-plan tests cover their existing public seams. The mod has tests in its own `tests` folder, excluded from installation; run them with `chimera mod test internal/engine/hooks/chimeramod/legato` on a mod-capable Chimera.

Until a mod-capable upstream build is available, event fixtures are explicitly synthetic v1 shapes, not recordings. The Go integration tests cannot certify the upstream loader, event dispatch or secret handshake by themselves. No running Legato instance is used for these tests.
