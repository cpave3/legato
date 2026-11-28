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

## CodeEval hooks for Claude Code

The repository runs [CodeEval](https://github.com/cpave3/code-analyzer) as Claude Code hooks, so a Claude Code session in a checkout gets them, including an agent that Legato starts in a job clone. CodeEval measures the structure of a change. It does not run code.

- **When a session starts,** the hook records the commit it started from, in `.git/codeeval/<session>/`. Each Claude session keeps its own, so two sessions in one checkout do not disturb each other. The hook also tells Claude the quick check to run before it pushes.
- **When Claude stops,** the hook checks what the session changed, committed or not, since its last stop that passed (or since it started). Work an earlier stop passed, and work merged from others, is not checked again. A quick check of the changed files runs in about a second. A broad check of the cross-file rules runs in the background, and the next stop reports its result.
- **When the check fails,** the hook blocks the stop once and gives Claude the findings. A change may add up to 10 points of debt before it fails; set `CODEEVAL_DEBT_BUDGET` to change that. Once the branch's pull request is approved, the hook only reports a finding to you and does not block.

The hooks look for CodeEval in this order: `$CODEEVAL_BIN`, then `~/.local/share/codeeval/current/codeeval`, then `codeeval` on `PATH`. When they find none, they do nothing and print nothing. You do not need CodeEval to work on Legato.

To turn the Stop check off, set `CODEEVAL_HOOK=off`, or `CODEEVAL_BIN=/bin/false`, in the environment you start Claude Code from. For a false positive, add a `codeeval-ignore <rule-id>: <reason>` comment on or above the line.

:::caution
The hook scripts are CodeEval 0.9.1's, unchanged. Running `codeeval hook install` in the repository updates them, and it also writes your machine's CodeEval path into `.claude/settings.json`. Commit the scripts if you mean to update them, but put back the two commands in `.claude/settings.json`.
:::

## Pull requests and CI

Every pull request runs the `ci` workflow, drafts included:

- `paths` checks that no two paths differ only in case.
- `migrations` checks that new database migrations sort last.
- `go`, `web` and `packages` run the Go tests and builds, the web UI's build and tests, and the checks of the apps under `packages/`.
- `docs` builds this site when the pull request changes it, and passes at once when it does not.
- `stack` posts the `ci/stack` status, which is explained below.

### Stacked pull requests

In a GitHub stack of pull requests, only the top pull request runs `go`, `web`, `packages` and `docs`. On the pull requests below it, these jobs are skipped, and `paths` and `migrations` still run. The `ci/stack` status tells you whether the code below the top has passed:

| `ci/stack` | What it means |
| --- | --- |
| success, "Gated by its own checks" | This is the top of the stack, or it is not in a stack. Its own checks give the result. |
| pending, "PR build running on #N" | The top pull request, #N, is building. Its build contains this pull request's code. |
| pending, "Waiting for the last PR in the stack" | This pull request changed after the top last built. Push the top again, after you restack it. |
| pending, "Waiting for approval on #N" | The top's build passed, but the top still needs an approval. |
| success, "PR build passed and approved on #N" | The top's build passed and contains this pull request's code. |
| failure, "PR build failed on #N" | The top's build failed. The status's link opens that run. |
| pending, "#N closed; unstack and link the stack again without it, …" | The top, #N, was closed without merging. See below. |

To open a stack so that only its top builds:

1. Open its pull requests as drafts.
2. Link them into a stack with `gh stack link`. The bottom pull request must be based on `main`.
3. Mark them ready for review.

Linking starts no workflow. Marking a pull request ready does, and by then GitHub knows the stack.

If you close the top pull request without merging it, GitHub keeps it in the stack, so the pull request below it is still not the top. Unstack the stack and link it again without the closed pull request, then push to the new top to build it in full. Re-running an old run does not work, because a re-run still sees the old position in the stack.

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

For a real packaging check, use JDK 21 and Android SDK platform 36 with build-tools 36.0.0:

```bash
bash scripts/test-android-apk.sh
```

It generates a disposable signing key and builds two versions with real Gradle, checking the packaged version, certificate and palette. It never installs or uploads an APK and does not prove phone data survives an upgrade.

:::caution
Official Android release support is implemented, but no official APK is published yet. No production signing setup has been performed. Cameron must choose the durable private signer first; fresh-install, same-signer upgrade preserving servers, pairing and approval keys, and real Obtainium skipped-release checks remain operator acceptance work.
:::

The release workflow accepts `android=auto|always|never`, default `auto`. `auto` compares against the most recently published stable APK-bearing release across all pages of GitHub releases. It builds on changes to exactly `mobile/`, `themes/palettes.json`, `scripts/mobile-android.sh`, `scripts/android-release.py` or `.github/workflows/release.yml`; unrelated server changes do not need a new shell APK. Missing baseline tags are fetched from `origin`, and lookup or comparison failures fail visibly rather than skip Android.

Official versions use stable `vMAJOR.MINOR.PATCH` tags without leading zeros. The APK's version name omits `v`; its version code is `(major + 1) * 1,000,000 + minor * 1,000 + patch` (major <= 2098, minor/patch <= 999). Publication below an already published Android version code is rejected.

The operator owns the `android-release` environment: secrets `ANDROID_KEYSTORE_BASE64`, `ANDROID_STORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`, and public variable `ANDROID_CERT_SHA256`. The helper requires the approved private certificate pin and rejects the bundled debug certificate. Signing changes and server trust in `approval_devices.android_app_digests` require explicit operator decisions, not automatic setup.

The canonical release assets are `legato-android.apk` and `legato-android-SHA256SUMS`, with GitHub build provenance. If Android fails after the CLI release is published, fix the prerequisite and rerun **only the failed Android job** at the same tag, never all jobs. Recovery stages and verifies the immutable existing APK's signature/version and regenerates a missing checksum without clobbering either asset.

SDK setup explicitly installs `platform-tools`, not the retired `tools` package, followed by platform 36 and Build Tools 36.0.0. Rerunning an old job keeps its original workflow revision: if the workflow code needs a fix, merge it and publish a new release with `android=always`. Do not move the old release tag.

GitHub can leave an empty `starter` placeholder after a failed upload. Recovery rechecks its exact identity and deletes only a canonical, zero-byte `starter` placeholder before retrying publication. Completed assets are never replaced. Other unexpected states or cleanup failures stop recovery for investigation; see the release runbook below.

See [`mobile/README.md`](https://github.com/cpave3/legato/blob/main/mobile/README.md#official-android-releases) for signing, certificate inspection and recovery, and [Devices](/legato/running/devices/#android-updates-with-obtainium) for Obtainium's bounded release discovery.

### Desktop app

A Tauri app in `desktop/`: Rust, with a React shell. It needs Rust, Node with pnpm, and on Linux the WebKitGTK 4.1, GTK 3, librsvg and libudev development packages. The AppImage carries the GStreamer plugins of the machine that builds it, so install GStreamer's base, good, GL and libav plugins, and `patchelf`, before `task desktop:build`. On Fedora the task points the bundler at `/usr/lib64/gstreamer-1.0`; after the build it checks that the AppImage holds the plugins it needs.

```bash
task desktop:test    # Rust unit tests and the TypeScript check
task desktop:dev     # run it, the shell reloading as you edit
task desktop:build   # an AppImage
```

See [The desktop app](/legato/running/desktop-app/) and [`desktop/README.md`](https://github.com/cpave3/legato/blob/main/desktop/README.md).

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

### Apps under packages/

Apps are moving into `packages/`, one folder each, in one pnpm workspace rooted there. The first is the Even Realities G2 glasses app in `packages/even-hub/`.

```bash
task packages:check        # lint, typecheck and test every app in packages/
task even-hub:dev          # the glasses app on its recorded fixture
task even-hub:sim          # the Even Hub simulator, open on it
task even-hub:screenshots  # run both and save screenshots (-- --headless for xvfb-run)
task even-hub:beta         # put the next build on your Even account's beta channel (-- -m "what changed")
```

See [Even Realities G2 glasses](/legato/integrations/even-g2/) and [`packages/README.md`](https://github.com/cpave3/legato/blob/main/packages/README.md).

## Release builds

Releases are built by a GitHub workflow, not by hand. `task release:dist VERSION=vX.Y.Z` builds the web UI once, then a binary stamped with that version for Linux on amd64 and arm64 and for macOS on arm64, with a `SHA256SUMS` file. [`docs/ai/updates.md`](https://github.com/cpave3/legato/blob/main/docs/ai/updates.md) describes the release workflow.
