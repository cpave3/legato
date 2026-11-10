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
