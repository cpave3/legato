# Chimera mod evidence

## Pinned host

Captured on 2026-11-14 from **cpave3/chimera `859884e7a69943df79124bed58f87c9f6f89a99f`**, the pushed, clean 18-ticket branch plus three upstream review fixes behind [Chimera PR #3](https://github.com/cpave3/chimera/pull/3). Binary: Linux amd64, Bun 1.4.4, runtime version `v0.0.0-local`.

SHA-256: `c1586c10f13ba850d76870262098ae7bd036c547e6fcc72e8aad43186144cbd7`.

CHIM-69 subsequently merged to Chimera main as `e6458b4c`, from this tested head. Mods v1 is on main but not yet in a released Chimera build.

These are **real runtime events**, not calls to the fake mod harness: `record.py` starts the production Legato mod in an isolated Chimera TUI, on a private tmux socket, with an empty home and a local scripted OpenAI-compatible provider. Its link child records stdin instead of contacting a Legato instance. Only `cwd` and `serverUrl` values are normalized; session/request/job IDs, ordering, reasons and runtime version are preserved. `cases.json` contains the hand-authored expected state/job observations.

`TestRecordedChimeraModEventsThroughStoreAndBroker` feeds all 16 streams into `RunChimeraLink`, against temporary SQLite and the actual Unix broker: **32 cases**, checking each record's activity, jobs, binding and saved conversation identity, then exit cleanup.

## Evidence matrix

| Path | Evidence |
| --- | --- |
| Initial turn, stop, `!cmd` append without a turn | `basic.ndjson` |
| Permission request/resolution | `permission.ndjson` |
| Question request/resolution | `question.ndjson`; canonical Chimera HTTP answer, not Legato's native-panel answer transport (see limitation below) |
| Background shell survives the turn, then exits | `background.ndjson` |
| Background subagent, finish and automatic continuation; no child session takes the binding | `subagent.ndjson` |
| Failed model run | `error.ndjson` |
| Step limit, output limit, interruption, asking for input | `max-steps`, `output-limit`, `interrupt`, `waiting-input` |
| `/new`, `/fork`, `/sessions` picker; one retained helper | `switch.ndjson` |
| Fresh process, then resume persisted conversation | `resume-initial.ndjson`, `resume.ndjson` |
| Double Ctrl+C, SIGTERM, SIGHUP | `ctrl-c`, `signal`, `hup`; signals target the actual Chimera process PID, not a process group |
| Queue limit/observer eviction, retry of the same sequence | Native `chimera mod test` queue/retry tests, with host write-promise injection; separate Node mock failure/deadline tests |
| API spawn → actual mod/link/broker/SQLite, MCP `task_context` round trip, saved identity | `end_to_end.py`, passed against the pinned host and a built Legato from this branch |
| No launch-time MCP flag, no server secret in argv/tmux, bootstrap consumed, anonymous HTTP 404/authenticated 200 | `end_to_end.py` |
| Restart scratch Legato without restarting its agent; deliver another message; final `/exit` unbind | `end_to_end.py` |
| Read-only mods/hooks from first spawn; linked sources; bootstrap mask and host preamble | Sandbox-plan/spawn tests and executable adapter test (`TestChimeraConsumesSecretBeforeEnteringSandbox`); full scratch app check uses `agents.sandbox.enabled: false` |
| Malformed/duplicate/out-of-order reports, overlapping requests, timeout reason, stale scripts and pane detection | Explicitly synthetic Go tests, complementing the captures |

**Pending: the 18-flake-point check.** The exact list belongs to LEG-179's withdrawn `chimera-mods` plan, which was not handed off and is unavailable on this worker. No substitute list or checked-off claims have been invented.

## Follow-up, not a passing claim

`CHIMERA_SERVER_SECRET` remains readable from `/proc/<pid>/environ` by Chimera's own unsandboxed commands. CHIM-86 tracks a file-descriptor/pipe handoff; the argv/tmux checks do not claim to close this environment exposure.

Legato's pre-existing Chimera question-answer transport sends the shared `{answers: map, dismissed}` format; this host requires `{status: "answered", answers: [{question, answer, custom}]}` or `{status: "cancelled"}`. Its raw question items also lack the per-question IDs Legato's native UI expects. The capture driver answers through Chimera's own endpoint so it can verify waiting/resolved state. **It does not prove that the native panel can answer a question.** That protocol adapter/UI migration remains a separate follow-up; the Claude bridge is unchanged.

The old 06:32 pre-publication build exposed two host bugs: write-promise injection was ignored, and a legacy signal listener bypassed async shutdown. Both were fixed upstream; native queue/retry and fresh TERM/HUP captures pass against the pinned published revision above. No old-build captures remain here.

## Reproduce

Requires Python 3, tmux, a built Legato and the pinned Chimera binary. The scripts create fresh config/database/data/runtime/home folders, use only loopback model/API servers, never install a binary globally, and stop/remove their own scratch instances afterwards.

```sh
# From the repository root; paths below name your test binaries.
CHIMERA=/path/to/pinned/chimera
LEGATO=/path/to/built/legato
RAW=$(mktemp -d)

CHIMERA_MOD_TEST_BIN="$CHIMERA" go test ./internal/engine/hooks -run TestChimeraModNative -count=1 -v
node --test internal/engine/hooks/chimeramod/mock.test.mjs

for case in basic permission question background subagent max-steps output-limit error interrupt waiting-input switch resume ctrl-c signal hup; do
  python3 internal/cli/testdata/chimera-mod/record.py --chimera "$CHIMERA" \
    --mod internal/engine/hooks/chimeramod/legato --out "$RAW" --case "$case"
done
python3 internal/cli/testdata/chimera-mod/normalize.py --raw "$RAW" --binary "$CHIMERA" \
  --revision 859884e7a69943df79124bed58f87c9f6f89a99f
go test ./internal/cli -run TestRecordedChimeraModEventsThroughStoreAndBroker -count=1 -v

python3 internal/cli/testdata/chimera-mod/end_to_end.py --legato "$LEGATO" \
  --chimera "$CHIMERA" --artifacts "$RAW"
```

Run heavy builds/full suites through the worker's shared lock. When running Go tests from a Legato-launched shell, clear `BASH_ENV`, `ZDOTDIR`, `LEGATO_USER_BASH_ENV` and `LEGATO_USER_ZDOTDIR`: the existing credential tests create their own shell wrappers, and nesting the launcher's wrappers makes their bash startup recurse. Use writable scratch Go/npm caches inside an OS sandbox.
