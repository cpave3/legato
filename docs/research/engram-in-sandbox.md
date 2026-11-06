# Engram in Legato's sandbox

Research for [#14](https://github.com/cpave3/legato/issues/14) on the map [#12](https://github.com/cpave3/legato/issues/12), *start Legato agent sessions as engrams*. The decision was already made: a sandboxed engram session gets the engram binary and the **whole engram store** (`ENGRAM_HOME`), read-write. This note says what that takes.

Sources: Legato at `81b2008` (`internal/service/sandbox_plan.go`, `internal/service/agent_sandbox.go`, `internal/engine/sandbox/sandbox.go`, `cmd/legato/sandbox_runtime.go`, `docs/ai/sandbox.md`); engram at `b51ee38` (github.com/cpave3/engram), and the installed binary, which `go version -m` reports as built from `bac134f+dirty` with `modernc.org/sqlite v1.58.0`. Experiments ran on Fedora Silverblue with bubblewrap 0.11.0, using bwrap argument lists rendered by `BuildSandboxPlan` itself (a throwaway Go test, not committed), against a throwaway `ENGRAM_HOME` with a throwaway identity. The real store was never written.

## Answer in short

| Question | Answer |
| --- | --- |
| The binary | Resolve the **real executable** on the host at spawn, never a shim, and bind it read-only at `<runtime>/legato/sandbox/<execution>/bin/engram`, with that folder first on the session's `PATH`. |
| `ENGRAM_HOME` | **Set it explicitly**, in the session's environment and for Legato's host `engram boot`, to the store root Legato mounts. The default works today only by coincidence. |
| Concurrency | **Safe.** Locks and WAL work across bwrap's namespaces because both sides use the same files. Checked with 12 concurrent writers, six of them sandboxed. One race exists, and it is engram's own (two first commands of one session). Legato's boot order avoids it. |
| What else | `git` (in `/usr`, always mounted). Engram also reads a few variables, all of which reach the sandbox. Legato's `insteadOf` rewrite of `origin` inside the sandbox does not change the scope. |
| Mechanism | **A new, small per-spawn input**, shaped like the `gh` one (`SandboxGH`), not `agentState` and not `Extra`. |
| Requests to engram | One command that says where the binary is and where the store is. Optionally, make starting a session atomic. |

## The binary

### Where it is, and whether the sandbox sees it

`engram` on this machine is `~/.local/share/mise/installs/go/1.26.0/bin/engram`. It is a `go install` under mise's Go (`GOBIN` points there), and that folder is on the pane's `PATH` because the person's shell runs `mise activate`. There is also a mise **shim**, `~/.local/share/mise/shims/engram`, which is a link to `~/.local/bin/mise`.

- With `default_mounts` on, `~/.local/share/mise` and `~/.local/bin` are in `toolchainsRO` (`sandbox_plan.go:198`), so the binary is visible read-only. The sandbox keeps the pane's `PATH`: bwrap inherits the environment, and the plan sets only `LEGATO_SANDBOXED` and `TMPDIR` (`sandbox_plan.go:333`). So `engram` resolves inside. **Verified**: under the rendered plan, `command -v engram` gave the mise install path, and `boot`, `remember`, `memories` and `recall` all worked.
- With `default_mounts` off, nothing mounts it. **Verified**: under the rendered plan, `engram` was missing.
- The binary is dynamically linked (`CGO_ENABLED=1` in its build info) and needs only `/lib64/ld-linux-x86-64.so.2` and libc. Both are in `/usr`, which every plan mounts.

### Shims are not safe to rely on

A mise shim runs `mise`, and mise picks the tool version from the **working directory's** mise configuration. In a sandboxed checkout whose `mise.toml` pins another Go:

```
repo with mise.toml (go 1.25):
mise go@1.25.14      [1/3] install
mise ERROR Failed to install core:go@1.25: failed create_dir_all: ~/.local/share/mise/installs/go/1.25.14: Read-only file system (os error 30)
real binary in the same repo:
probe — Throwaway identity for sandbox research.
```

In a plain repository, the shim worked inside: mise wrote its state into the empty home and read `~/.config/mise` read-only. Whether a shim works therefore depends on the repository, so Legato must resolve past it.

Covering the shim with the real binary does not work either. bwrap cannot bind a file onto a path that is a symbolic link (`bwrap: Can't create file at …/shims/engram: No such file or directory`). The `gh` approach of binding over each *resolved* path (`sandboxGH`, `agent_sandbox.go:188`) would bind over `~/.local/bin/mise` itself and break mise in the sandbox.

### Recommendation

1. **Find it on the host**, at the same time as the probe (`engram.command`, else `exec.LookPath("engram")` on Legato's `PATH`). Then resolve it to the real executable:
   - with engram's help (request 1 below), ask engram for its own `os.Executable()`. That goes through any shim or link, of any version manager.
   - until then, use `filepath.EvalSymlinks`. If the result is the `mise` binary, run `mise which engram`. If it is a script (an asdf-style shim), refuse and ask for `engram.command` to name the binary.
   - use the same resolved path for the host `engram boot`, so boot and agent run the same engram.
2. **Mount it** read-only at `<runtime>/legato/sandbox/<execution>/bin/engram`. Make an empty file there first, as `gh-real` is made (`agent_sandbox.go:71`). Then put that folder **first** on the sandbox's `PATH` (a `Setenv` of `PATH` computed from the session's). The execution's sandbox folder is already `allowedInside` (`sandbox_plan.go:265`). The folder holds only `engram`, so it changes nothing else's resolution. **Verified**: with `default_mounts` off, and `PATH` set to that folder plus `/usr/bin`, `command -v engram` found the bind and `memories` read the store.
3. This works with `default_mounts` on or off, and wherever engram is installed (`~/go/bin`, mise, a linuxbrew keg, a checkout). It does not need the install's folder to be visible.

Inside, no user startup files run (the home is empty), so nothing adds a shim folder ahead of the per-execution folder. A file bind keeps the file the spawn saw. If engram is reinstalled by replacing the file, a running sandbox keeps the old one until it is reloaded, as with sockets (see `docs/ai/sandbox.md` § Known limits).

## The store and `ENGRAM_HOME`

Engram resolves its root as `ENGRAM_HOME`, then `$XDG_DATA_HOME/engram`, then `$HOME/.local/share/engram` (`internal/cli/cli.go`, `homeDir`). Inside the sandbox, `HOME` and `XDG_DATA_HOME` are the pane's values, and the empty home is a tmpfs at the same path (`--tmpfs <home>`). So the default root is **the same path** inside as outside. It holds the store only if that exact path is bound. This session's own sandbox shows the store bound read-write that way (`/proc/self/mountinfo`: `/home/cameron/.local/share/engram … rw`), most likely through an `agents.sandbox.mounts` entry.

**Set `ENGRAM_HOME` explicitly**, to the real path Legato mounts, in the session's environment and in the environment of the host `engram boot`. Reasons:

- Legato's own process, the tmux server and the pane may disagree on `XDG_DATA_HOME`. For example, Legato may run as a service while the person's shell sets it. Then the host boot and the agent would use different stores, and the agent's writes would go into the tmpfs home and be lost when the pane ends.
- It makes the mount and engram's root the same value by construction, and it costs nothing for unsandboxed sessions.
- Resolve the root on the host by engram's own rule. Request 1 lets engram say it.

Other notes on the store:

- `add` silently skips a path that does not exist (`sandbox_plan.go:337`). That is harmless here: with `ENGRAM_IDENTITY` or `--as` set, engram fails loudly on an empty root (`engram: unknown identity "probe" (none issued yet…)`, exit 1). Still, Legato should refuse the spawn when the root it resolved is missing. Legato only picks existing engrams, so the root exists in normal use.
- The store must pass `check` (`sandbox_plan.go:322`): an `ENGRAM_HOME` inside Legato's data folder or another forbidden path is refused. The default `~/.local/share/engram` is not forbidden.
- Bound at its real path, the store works whichever spelling `ENGRAM_HOME` uses (`/home/…` or `/var/home/…`), because the sandbox recreates `/home` as the same link.

## Concurrency

### What engram does (source)

- **Plain files.** Every record is written to a temp file in the same folder and then `rename`d (`internal/store/store.go`, `writeFile`). New records get random names. Session folders are made with `os.Mkdir`, which fails if the folder exists. A session's journal is appended with `O_APPEND`, one `write` per line (`internal/store/session.go`, `Append`). `self write` keeps every version in `self-history/` (`internal/store/identity.go`, `WriteSelf`). Nothing in the store takes a lock.
- **The index.** Each identity has `.index/memories.sqlite` (`internal/cli/memory.go:326`), a derived database that can be deleted. It is opened with `journal_mode(WAL)`, `busy_timeout(5000)`, `_txlock=immediate` and one connection, and it retries `SQLITE_BUSY`/`LOCKED` 40 times, 25 ms apart (`internal/index/index.go`, `Open`, `retryBusy`). `Sync` takes the write lock only when files changed.

### Why bwrap does not change it

The sandbox binds the **same files** (same filesystem, same inodes) at the same path. SQLite's locks are `fcntl` record locks on those files, and the kernel holds them per file, whatever namespaces the processes are in. WAL coordinates through `memories.sqlite-shm`, which every process maps. That file is one shared file, so every process sees one shared memory region. bwrap's fresh PID, IPC, UTS and cgroup namespaces do not affect either mechanism: WAL does not use System V IPC, and SQLite does not use PIDs for locking. The two things that break SQLite locking are network filesystems and copies of the file, and neither is in play.

### How it was checked

Six host-side workers and six sandboxed workers ran together against one throwaway store. Each sandboxed worker was its own `bwrap` under the rendered plan, so each had its own PID namespace (every one reported PID 2). Each worker ran 100 rounds of `remember`, `note` and `recall`. The index was deleted first, so both sides raced to create and migrate it.

```
box-1..6 failures=0    host-1..6 failures=0    stderr lines: 0
memory files: 101 -> 1301      (1200 remembered)
journal lines: 202 -> 2602     (1200 notes + 1200 remember events, one shared session)
unparseable journal lines: 0
index docs: 1301               integrity: ok        journal_mode: wal
```

Every write landed, no journal line was torn, the index matched the files exactly, and `PRAGMA integrity_check` was `ok`.

### The one race, which is engram's

When the **first** commands of one session key run at the same time, engram can start several sessions. `SessionForKey`, then `StartSession`, is check-then-create (`internal/cli/session.go`, `session`). With four simultaneous first `boot`s on one fresh `ENGRAM_SESSION`, host-only gave 4 sessions out of 4 tries, every time. Mixed host and sandbox gave 2 (sandbox start-up time spread them out). The sandbox does not cause this. It does not affect Legato either: Legato runs `engram boot` on the host and waits for it **before** the agent starts, so the key file exists before the agent's first command. Concurrent sessions of different executions use different keys and never race.

## What else engram needs

- **git**, for the scope (`internal/cli/scope.go`): `git -C <cwd> remote get-url origin` and `git rev-parse --path-format=absolute --git-common-dir`. `/usr/bin/git` is in `/usr`. The working directory, and a worktree's main `.git`, are mounted at their real paths. Outside a repository, the scope is the directory, with links resolved.
- **The `origin` rewrite.** With a GitHub credential, a sandboxed session gets `url.https://github.com/.insteadOf git@github.com:` (`sandboxGitFetches`, `credential_github.go:356`), and `git remote get-url` applies `insteadOf`. So inside, `origin` reads `https://github.com/cpave3/legato.git`, and on the host it reads `git@github.com:cpave3/legato.git`. Engram's `remoteKey` reduces both to `github.com/cpave3/legato`. **Verified**: a memory saved inside was `here` from the host, and the reverse. `pushInsteadOf` (the proxy URL) does not affect `get-url` without `--push`, so the proxy's address never becomes a scope. The person's `~/.gitconfig` is hidden in the sandbox. If it holds an `insteadOf` of its own (a shorthand like `gh:`), the host and the sandbox could name a repository differently. That is an edge case worth one line in the docs.
- **Environment.** Engram reads `ENGRAM_HOME`, `XDG_DATA_HOME`, `HOME`, `ENGRAM_IDENTITY`, `ENGRAM_SESSION`, `ENGRAM_HARNESS`, `ENGRAM_MODEL`, `CLAUDECODE`, `CLAUDE_CODE_SESSION_ID`, `LEGATO_EXECUTION_ID`, `LEGATO_TASK_ID`, `TMUX_PANE` and `TMUX` (`grep Getenv`). Only `TMUX` and `TMUX_PANE` are in `secretVars` (`sandbox_plan.go:213`). Engram consults them only after `LEGATO_EXECUTION_ID`, so the session key is `legato:<execution>` inside and out. **Verified**: a session started in the sandbox was `resumed` by a host-side `boot` with the same execution ID. So Legato's host `engram boot` must run with the session's `LEGATO_EXECUTION_ID`, `LEGATO_TASK_ID` and `ENGRAM_HOME`, in its working directory.
- **Nothing else**: no network, no sockets, no files outside the store and the working tree. `/tmp` is not used (temp files go in the target folder).

## Which mechanism should carry it

| Mechanism | Fit |
| --- | --- |
| `agentState` (`sandbox_plan.go:206`) | No. It is keyed by agent tool, relative to home, and mounted for every session of that tool. Engram is a per-spawn choice, independent of the tool. |
| `Extra` (`agents.sandbox.mounts`, plus resources appended per spawn at `agent_sandbox.go:57`) | Half. It can carry the store (`write`), and an `agents.sandbox.mounts` entry is today's workaround. It has no `Dest`, so it cannot put the binary on `PATH`, and it silently skips a missing path. It also mixes Legato's own needs into the person's configured mounts. |
| **New: an `Engram` input**, like `SandboxGH` | **Yes.** It is set per spawn in `sandboxPrefix` only for engram sessions, and it is independent of `default_mounts`. |

Sketch:

```go
// SandboxEngram is what an engram session's sandbox needs.
type SandboxEngram struct {
	Binary    string // the real executable, resolved on the host
	BinMount  string // <runtime>/legato/sandbox/<execution>/bin/engram
	Store     string // ENGRAM_HOME, resolved on the host; must exist
}
```

In `BuildSandboxPlan`: `check(Store)`, then a read-write bind of `Store`, failing when it is missing. Then a read-only bind of `Binary` with `Dest: BinMount`, and `Setenv["PATH"] = dir(BinMount) + ":" + <session PATH>`. In `sandboxPrefix`: create the empty `BinMount` file. `ENGRAM_HOME` and `ENGRAM_IDENTITY` go in the **session's** environment, which bwrap passes through, so unsandboxed sessions get them too. Test it the way `sandbox_plan_test.go` tests the `gh` overlay. Add a real-`bwrap` case beside `sandbox_bwrap_test.go` that runs `engram` against a temp store.

## Requests to engram

1. **Say where you are.** Add a machine-readable answer, giving the resolved `os.Executable()` and the store root engram would use (`homeDir`), and ideally the version. Either a small `engram where --json`, or `binary` and `home` fields added to the `identity list --json` output that Legato's probe already reads. With that, Legato resolves any shim or link without knowing about version managers, and it mounts exactly the root engram uses. *(Needed for a clean implementation. Legato can manage without it.)*
2. **Make starting a session atomic.** Two first commands of one key can start two sessions (see § The one race). Create the key file with `O_EXCL`, or take a short lock, and re-read it on a clash. *(Nice to have. Legato's boot order avoids the race.)*
3. *(Optional)* Build releases with `CGO_ENABLED=0`. The binary is already pure Go (`modernc.org/sqlite`). A static binary needs no loader or libc, which helps sandboxes that mount less than all of `/usr`.
