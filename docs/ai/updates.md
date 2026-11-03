# Releases and updates

A Legato built from a GitHub release keeps itself on the newest stable release. A Legato built from source says it is `dev` and never updates itself: that is how a developer's checkout and a worker that runs releases live side by side.

## The version

`internal/engine/version` holds the version a release build stamps in (`-X github.com/cpave3/legato/internal/engine/version.stamped=vX.Y.Z`); without it the version is `dev`. `version.IsRelease` is true for a stable `vX.Y.Z` only, `version.Newer` compares two. The version is in `legato version` (`--json`: `{version, release}`; it reads no configuration), `GET /health`, `GET /api/meta`, the MCP server's handshake and the 1Password integration's name.

## Making a release

`.github/workflows/release.yml`, run by hand on `main`:

```bash
gh workflow run release -f bump=patch    # or minor, major
```

1. `scripts/next-version.sh <bump>`: the newest stable tag (`vX.Y.Z`, compared as numbers; prereleases and other tags left out), bumped. The first release is `v0.0.1` (patch) or `v0.1.0` (minor).
2. `go vet ./...` and `go test -short ./...`.
3. `task release:dist VERSION=vX.Y.Z`: the web UI once, then `task build:release` for `linux/amd64`, `linux/arm64` and `darwin/arm64` into `dist/legato-<os>-<arch>` (`CGO_ENABLED=0`, `-trimpath -s -w`), and `dist/SHA256SUMS`. No Mac is needed: Legato builds without C, and Go's linker signs a `darwin/arm64` binary ad hoc, which Apple Silicon requires. The binaries are not notarized.
4. The Linux amd64 binary must say it is the version it was built as.
5. Build provenance (`actions/attest-build-provenance`) for each binary, then `gh release create vX.Y.Z` with the binaries and `SHA256SUMS`, its notes generated from the commits. The tag is the version: nothing is committed back to `main`.

The asset names have no version in them, so `https://github.com/cpave3/legato/releases/latest/download/legato-linux-amd64` is always the newest.

## The updater

`service.UpdateService`, wired in `cmd/legato/update_runtime.go` for `legato serve` and for the TUI's web server.

- **Mode**, kept in `instance_values` as `updates.mode`: `auto`, `ask` (the default) or `off`. `PUT /api/settings/updates {mode}`, Settings › Updates.
- **Checks**: a minute after start, then every 6 hours, and at once when the mode changes; never in `off`. `selfupdate.Client.Latest` reads GitHub's latest release of `cpave3/legato`, which is never a draft or a prerelease, without a token (60 requests an hour).
- **ask**: the web UI's bar along the top says the release is out (`UpdateNotice`; "Later" hides it in that browser until the next release), and Attention has an item (`updates` producer, `update_available`) with **Update and restart**. Either calls `POST /api/updates/apply`.
- **auto**: installs once no agent is in the middle of a turn (`AgentsMidTurn`: an agent `working`, or `background` with work that will wake it; one `waiting` for the person is not), asking every 30 seconds, and after 3 hours installs whatever the agents are doing.
- **Installing** (`selfupdate.Client.Install`): the binary for this OS and CPU is downloaded beside the running one (links followed, so `~/.local/bin/legato` pointing at a file elsewhere works), checked against `SHA256SUMS`, made executable and run (`<new> version --json` must say the release's version), then the running binary is kept as `<binary>.prev` and the new one renamed over it. Any failure leaves the running binary as it was, and the state `failed` with the reason; Attention offers **Try again**.
- **Restarting** (`processRestarter`):
  - under the hot-reload supervisor, which restarts its child when the binary changes, nothing more is done;
  - as the main process of a systemd service (`SYSTEMD_EXEC_PID` is Legato's own PID; a shell in a terminal that runs as a service inherits another), `legato serve` shuts down as on SIGTERM a second later and exits with status 75, which `Restart=on-failure` (and `RestartForceExitStatus=75`) starts again. `KillMode=process` keeps tmux and every agent running; Legato finds them again when it starts;
  - otherwise (the TUI, or `legato serve` by hand) the update is `installed` and waits: the bar says to restart Legato.
- **API**: `GET /api/updates` (`service.UpdateStatus`: `version`, `release`, `mode`, `state`, `available`, `latest`, `latest_url`, `checked_at`, `error`; `503` without an updater), `POST /api/updates/check` (`502` with GitHub's refusal), `POST /api/updates/apply` (`202`, then the update goes on in the background; `409` with nothing newer or for a build from source). `GET /api/settings` carries the same status as `updates` (absent without an updater), so the web UI's bar costs no request of its own. `EventUpdateChanged` becomes the `update_changed` WebSocket message, which refetches the settings.
- **States**: `unchecked`, `up_to_date`, `available`, `waiting` (for the agents), `installing`, `restarting`, `installed` (waits for a restart), `failed`.
- **Tools kept current: `st`.** `UpdateOptions.Tools` holds a `ToolUpdater` for Staccato's `st` (`service.NewSTUpdater`, `update_tools.go`). At each check (and on `POST /api/updates/check`) it finds `st` on Legato's `PATH` (links followed), asks `st version --json`, and for a release build `st update --check --json`; `st` downloads and verifies its own release, Legato only runs it, each command for at most 2 minutes. A `dev` or `go install` build, or an `st` without `version`, is `unmanaged` and never changed; none on the `PATH` is `missing`; Legato installs none. It follows the mode, except that `auto` installs at once, without waiting for agents: `st update` renames its file into place, so a run under way is not disturbed. `ask` offers it in Settings › Updates (**Update st to …**) and in Attention (`tool-update:st`, `update_available`), and `POST /api/updates/tools/{name}/apply` installs it (`202`; `409` for a tool not looked after or with nothing newer). A failure is the tool's own `failed` with its last line of output, and does not touch Legato's state. `UpdateStatus.tools` carries `{name, path, managed, version, latest, available, state, error, checked_at}`, `state` also `missing` or `unmanaged`. A Legato built from source still runs the loop for its tools, and checks nothing of its own.

## Known limits

- Agents use the `legato` binary too (hooks, `legato mcp serve`, the native bridges). After an update, new calls run the new binary while processes already running keep the old one, and a sandboxed agent keeps the binary its sandbox mounted until it is reloaded. Waiting for a quiet moment keeps the mix short; the protocols between them must still tolerate one version of difference.
- Migrations only go forward. `<binary>.prev` brings back a binary that does not start, not a database the new one migrated.
- The TUI's own Attention queue does not list the update; the web UI's does.
