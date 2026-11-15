# Real Claude integration health runner

`claude_end_to_end.py` launches a locally built Legato and **real Claude Code**
against a loopback Anthropic-compatible fixture. It exercises the installed
state/tools/native plugins, Go bridge, SQLite-backed aggregate list, actual
MCP task_context, and native HTTP/WebSocket UI payload paths. It does not
replace integration code or use provider credentials.

Requires Linux, Claude Code (verified 2.1.322), Node with global WebSocket, tmux,
Python 3 and a locally built Legato binary. From the repository root:

```sh
flock "$HOME/.nuget/packages/.legato-heavy.lock" \
  sh -c 'CGO_ENABLED=0 go build -o /tmp/legato-health ./cmd/legato'
python3 internal/cli/testdata/chimera-mod/isolated_run.py \
  python3 internal/cli/testdata/agent-health/claude_end_to_end.py \
  --legato /tmp/legato-health --artifacts /tmp/claude-health-evidence
```

Use the existing shared cache lock, not a private `/tmp` replacement. The
whitelist runner clears inherited session/broker authority before running the
driver. The driver creates private HOME/CLAUDE_CONFIG_DIR/config/database/data/
runtime/tmux/TMPDIR directories and independent ports, and asserts child output
stays beneath its private temporary directory. It pre-approves only the
synthetic `fixture-not-a-secret` provider key in its own disposable profile.
The scratch agent is unsandboxed; no sandbox/security regression is claimed.

## Scenarios

- Launch, final reply, aggregate idle and actual MCP task identity.
- Permission allow/deny controls a real nonsecret file; two-question option and
  custom answers plus dismissal use the UI's native WS/tray HTTP payloads.
- Three cycles of four concurrent messages across clean-stop boundaries:
  every marker occurs once in native history and in actual provider input.
- Three disconnected turns replay without duplicate event IDs.
- Active interrupt and reload: new execution, retained conversation history,
  persistent viewer follows the binding, MCP works after resume.
- Real async child success and stop, with parent independently idle/background;
  a launch result must not announce completion while the child runs.
- Completed API400 failure reaches live native errors and snapshot history,
  then a subsequent successful turn clears error activity.
- Unsupported modes are listed empty and explicitly rejected. The proxy's
  current error is 502 with upstream 409 Conflict, not a body-specific reason.

Claude 2.1.322 advertises always-asynchronous `Agent`, without the historical
`run_in_background` option. Do not count sending that removed flag as a
foreground-child test. Tool-boundary queue races and daemon handoff require
separate evidence; see the [scenario matrix](../../../../docs/analysis/agents-health-3jhyll8b.md).

Checks and waits are bounded. Success prints named outcomes; a failed check
returns nonzero and writes scratch-only diagnostics to `--artifacts`. URLs and
the scratch API token are redacted. WebSocket credentials enter `ws_probe.mjs`
on stdin, not argv. Cleanup stops only owned scratch processes and removes its
private root; retained evidence contains no real credentials. The loopback
fixture deliberately continues serving harmless queued response threads after
client cancellation, accepting connection reset as expected behavior.
