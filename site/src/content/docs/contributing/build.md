---
title: Build from source
description: Build, run and test Legato from a checkout, with or without the web UI.
sidebar:
  order: 10
---

Build Legato from source to work on it, or to run a version that is not released yet. One command builds the `legato` binary; a second builds the web UI into it first.

## Prerequisites

- **Go 1.25** or later, the version in [`go.mod`](https://github.com/cpave3/legato/blob/main/go.mod).
- **Task**, from [taskfile.dev](https://taskfile.dev). Every command on this page is a Task target in [`Taskfile.yml`](https://github.com/cpave3/legato/blob/main/Taskfile.yml).
- **Node.js and pnpm**, for the web UI and this documentation site. The CI uses Node 24 and pnpm 11.
- **tmux**, to start agents from the board. You can build and run Legato without it.
- **golangci-lint**, for `task lint` and `task check`. `task install:tools` installs it.

You do not need a C compiler. Legato builds with `CGO_ENABLED=0` into one static binary, and cross-compiles with `GOOS` and `GOARCH` alone. A test fails when a dependency starts to need C. The race detector is the exception: `task test:race` needs C.

## Build

Clone the repository and build:

```bash
git clone https://github.com/cpave3/legato.git
cd legato
task build
```

`task build` writes `./legato` with the TUI and the API, but no web UI. Without the web UI, `legato serve` answers API requests only.

To include the web UI, build it first, then the binary:

```bash
task build:full
```

`task build:full` runs `task web:build`, which installs nothing: run `pnpm install` in `web/` once before your first build. The web UI is built into `internal/server/static/dist/`, and the Go build embeds that folder in the binary.

### Install the binary

- `task install` installs a copy of `legato` into `$GOPATH/bin`. It embeds the web UI only if you built it before.
- `task link` builds `./legato` and links it into `$GOBIN`, or `$GOPATH/bin` when `GOBIN` is not set. Each later `task build` updates the linked binary in place.

### Version and updates

A build from source reports its version as `dev`:

```bash
./legato version
```

A `dev` build never updates itself, so a checkout and an installed release can run side by side. See [Updates](/legato/running/updates/).

:::note
Agents that Legato launches run the same `legato` binary for their hooks and tools. After you rebuild it, new calls run the new binary, while processes that are already running keep the old one.
:::

## Run with hot reload

Hot reload restarts Legato each time you rebuild its binary. Build once, then start the TUI or the web server under the supervisor:

```bash
task build
task run:hot      # the TUI, with its web server if configured
task server:hot   # the web server alone
```

`task server:hot` listens on port 3080, or on `LEGATO_SERVER_PORT`. These run `./legato --hot-reload` and `./legato --hot-reload serve`.

1. Change the code.
2. Run `task build`, or `task build:full` when you changed the web UI.
3. The supervisor stops the old process, waits for it to shut down, and starts the new binary with the same arguments.

A build that fails leaves the running process alone. The TUI loses its place on each restart. After `task build:full`, open web pages see the new web UI and reload once.

Under hot reload, the web UI's Settings shows **Rebuild and hot reload**, which runs `task build:full` in the checkout. Use it to rebuild from a phone. Its output goes to `.legato/run/hot-reload-build.log`.

Hot reload is for development in the foreground. It is not a way to run Legato as a service.

## Test

```bash
task test         # all Go tests
task test:race    # all Go tests with the race detector
task test:cover   # all Go tests, with coverage
task vet          # go vet
task lint         # golangci-lint
task check        # build, test, vet and lint
task web:test     # the web UI's tests
```

Run `task test:race` when you change the event bus or other code that runs at the same time.

Go tests use real SQLite databases in temporary folders, not mocks. Tests that need tmux skip when it is not installed.

## Work on the web UI

The web UI is a React and Vite app in `web/`. Run a Legato web server on port 3080, then start the Vite dev server:

```bash
task web:dev
```

The dev server reloads as you edit, and sends API and WebSocket requests to `localhost:3080`.

In `web/`, check types with `pnpm exec tsc -b`, the same check the build runs.

## Work on these docs

The documentation site is a Starlight site in `site/`. Both tasks install its dependencies first when they changed:

```bash
task docs:dev     # serve the site with live reload at http://localhost:4321/legato/
task docs:build   # build it into site/dist; fails on a broken link
```

[`site/README.md`](https://github.com/cpave3/legato/blob/main/site/README.md) is the style guide and the map of pages.

Two pages come from the code. The [keyboard reference](/legato/reference/keyboard/) is written from the TUI's key bindings by `go test ./internal/tui/hotkey -update`. `go test ./config` fails when a setting is missing from the [configuration reference](/legato/reference/configuration/).

## Other parts of the repository

### Android app

A native Kotlin app in `mobile/`. It needs a JDK from 17 to 24 and the Android SDK, but no Node.

```bash
task mobile:test      # unit tests
task mobile:apk       # release APK
task mobile:install   # build, install on the connected device, and start
```

See [`mobile/README.md`](https://github.com/cpave3/legato/blob/main/mobile/README.md) for signing.

### Stream Deck plugin

A Go plugin for OpenDeck in `streamdeck/`.

```bash
task streamdeck:build     # build the plugin binary
task streamdeck:package   # build and assemble the .sdPlugin folder
task streamdeck:dev       # build, deploy to OpenDeck and restart the plugin
```

See [Stream Deck](/legato/integrations/stream-deck/).

### Browser extension

A Chrome extension in `browser-extension/`. Run `pnpm install` there once.

```bash
task extension:build   # build the unpacked extension into browser-extension/dist
task extension:test
task extension:lint
```

See [Browser extension](/legato/integrations/browser-extension/).

## Release builds

Releases are built by a GitHub workflow, not by hand. `task release:dist VERSION=vX.Y.Z` builds the web UI once, then a binary stamped with that version for Linux on amd64 and arm64 and for macOS on arm64, with a `SHA256SUMS` file. [`docs/ai/updates.md`](https://github.com/cpave3/legato/blob/main/docs/ai/updates.md) describes the release workflow.
