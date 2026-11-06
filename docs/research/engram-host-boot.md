# What engram needs for Legato to boot a session on the host at spawn

Research for [#13](https://github.com/cpave3/legato/issues/13), on the map [#12](https://github.com/cpave3/legato/issues/12) (start Legato agent sessions as engrams). Written 2026-11-06.

## Answer in brief

Host boot works with engram as it is. One Legato-side rule is load-bearing: **the boot and the agent must agree on the session key and the store path.** The key is `LEGATO_EXECUTION_ID` today. Inside the sandbox, the store is found only when it is mounted at the same absolute path. With that in place, `engram boot` on the host and `engram note`/`remember` inside bubblewrap land in the same engram session, and the HOME the sandbox gives the agent makes no difference. That was checked with a real `bwrap`.

Two gaps need a decision:

- **A restart breaks the session.** `RestartAgent` makes a new execution ID, so a resumed conversation's later engram calls start a new engram session. The packet in its context names the old one. Legato fixes this by setting `ENGRAM_SESSION` itself, saved with the execution and carried over on a resumed restart.
- **The packet's "Using engram" section is a command reference, not a working practice.** For engram to own the guidance, it needs a section that says when to note, remember, open threads, re-boot and hand off.

## Sources

- engram source, `github.com/cpave3/engram` at `b51ee38` (2026-10-26), cloned into a scratch directory. Paths below are relative to that repository.
- The installed `engram` (`go version -m`: built from `bac134f+dirty`). Its `engram help`, `help concepts`, `help boot`, `help note` and `help handoff` output matches `b51ee38`. Every experiment was also run against a build of `b51ee38`, with the same results.
- Legato at `81b2008`: `internal/service/agent.go`, `agent_restart.go`, `agent_sandbox.go`, `sandbox_plan.go`, `credential_github.go`, `internal/engine/sandbox/sandbox.go`, `internal/engine/hooks/*.go`.
- Experiments: shell scripts against throwaway `ENGRAM_HOME`s under a temp dir. Packet sizes were measured on a **copy** of the real store (`cp -a`, so the real store was only read). The real store was never written.

## 1. Does the host boot's session become the agent's session?

**Yes, as long as both use the same key and the same store.**

How engram finds a session (`internal/cli/session.go:19` `ambientKey`):

| Order | Variable | Key | Strength |
|---|---|---|---|
| 1 | `ENGRAM_SESSION` | `explicit:<v>` | strong |
| 2 | `LEGATO_EXECUTION_ID` | `legato:<v>` | strong |
| 3 | `CLAUDE_CODE_SESSION_ID` | `claude:<v>` | strong |
| 4 | `TMUX_PANE` (+ server PID from `TMUX`) | `tmux:…` | weak |
| 5 | working directory | `cwd:<sha256[:6]>` | weak |

A strong key always resumes its session, with no age limit and even after a handoff (`session.go:73` `resumable`). The key→session binding is a file, `identities/<name>/sessions/.keys/<key>` (`internal/store/session.go:62`). Any engram command starts a session when there is none, so boot is not required first (`session.go:43`).

How engram finds its store (`internal/cli/cli.go:63` `homeDir`): `ENGRAM_HOME`, then `$XDG_DATA_HOME/engram`, then `$HOME/.local/share/engram`.

Legato's sandbox keeps the HOME **path** but puts an empty tmpfs over it (`internal/engine/sandbox/sandbox.go:112`, `--tmpfs <home>`). The environment passes through except the `secretVars` (`internal/service/sandbox_plan.go:213`), which include no `ENGRAM_*`, `XDG_*` or `LEGATO_EXECUTION_ID`.

Experiment (`bwrap --ro-bind / / --tmpfs <fake HOME>`, the store under the fake HOME, host boot with `ENGRAM_IDENTITY=ada LEGATO_EXECUTION_ID=ex-A`):

| Case | Result |
|---|---|
| Sandbox, store **not** mounted | `engram: unknown identity "ada" (none issued yet…)`, exit 1 |
| Sandbox, store bound read-write at the same path, no `ENGRAM_HOME` | `noted · session <same id as boot>` |
| Same, with `ENGRAM_HOME` set explicitly | same session |
| Same, with `HOME=/tmp/elsewhere` and `ENGRAM_HOME` set | same session |
| Same, no `ENGRAM_IDENTITY` (key only) | same session; the identity is found from the key |
| Same, from a subdirectory of the repo | same session |
| Store bound **read-only** | `open …/journal.jsonl: read-only file system`, exit 1 |
| `recall` in the sandbox (SQLite index under `.index/`) | works with the store read-write |

The scope also agrees. engram names the place by `git remote get-url origin` (`internal/cli/scope.go:68`). Inside the sandbox Legato adds `url.https://<host>/.insteadOf` rewrites (`credential_github.go:356` `sandboxGitFetches`). `remoteKey` reduces the SSH and HTTPS forms to one key, so a boot run with those rewrites still reported `Here is repo:github.com/example/widget`. One caveat: if Legato ever rewrites **fetch** URLs to its proxy (`http://127.0.0.1:…/c/github/git/…`), engram's scope would become `127.0.0.1/…` and split from the host's.

**Risk: an inherited `ENGRAM_SESSION` overrides `LEGATO_EXECUTION_ID`.** With `ENGRAM_SESSION=stale LEGATO_EXECUTION_ID=ex-A`, the note went to a different session. Legato's tmux panes get the tmux server's global environment plus `-e` values (`internal/engine/tmux/tmux.go:55`). A host boot gets whatever Legato's own process has. Legato should therefore set `ENGRAM_SESSION` (and `ENGRAM_IDENTITY`, `ENGRAM_HOME`) explicitly in both places, and never let them come from its own environment.

## 2. Which variables should Legato set?

Set the same values on the host boot and in the session's `envMap` (so `tmux -e` carries them, and bwrap passes them through):

| Variable | Value | Why |
|---|---|---|
| `ENGRAM_IDENTITY` | the chosen engram | Settled. Also makes `--as` unnecessary, though the packet prints `--as <name>` in every example anyway. |
| `ENGRAM_HOME` | the store path, resolved as engram does (`ENGRAM_HOME` → `XDG_DATA_HOME/engram` → `~/.local/share/engram`) with links resolved | The sandbox mounts that path. An explicit value means a sandbox that changes HOME or XDG still finds it (case "HOME=/tmp/elsewhere" above). See request R1. |
| `ENGRAM_SESSION` | a key Legato owns and saves with the execution: the execution ID at first spawn, the saved value on a resumed restart | Restart continuity (§5), and it overrides any stale inherited `ENGRAM_SESSION`. Note that `ENGRAM_SESSION=ex-A` gives key `explicit:ex-A`, which does **not** match a session started under `LEGATO_EXECUTION_ID=ex-A` (`legato:ex-A`). Legato must use one or the other from the start. |
| `ENGRAM_HARNESS` | the adapter name: `claude-code`, `codex`, `chimera` | `meta.json` is written once, when the session starts (`store/session.go:75`), and that happens at host boot, before the tool exists. engram only guesses `claude-code` from `CLAUDECODE=1` (`session.go:119`), which the host has not set. Codex and Chimera have no detection at all. |
| `ENGRAM_MODEL` | `opt.Model` when set (`agent.go:132`, passed to `LaunchCommand` at `:1017`); otherwise unset | Recorded in `meta.json` only. Legato does not know the tool's default model. |
| `LEGATO_TASK_ID`, `LEGATO_EXECUTION_ID` | as now | engram records both in `meta.json`. `LEGATO_TASK_ID` comes from `adapter.EnvVars` (`hooks/claude_code.go:161`, `codex.go:205`, `chimera.go:493`), so the host boot's environment must include it explicitly. In the experiments, a boot that did not set it recorded the **parent** session's task ID, which leaked in from the environment. |

The working directory for boot is the spawn's `workDir`, which is the job clone when there is one. It must exist before boot runs. The clone is made at `agent.go:603`, before the prompt is built.

Where it fits in `SpawnAgent`: `executionID` is generated at `agent.go:594`, before the environment and prompt are built. Boot can use it even though `LEGATO_EXECUTION_ID` is only put into `envMap` at `:824`. Boot must run before `writeAgentPromptFiles` (`:758`).

## 3. Packet size and budget

The budget counts 4 bytes as a token. The default is 3000 tokens, which is 12,000 bytes (`internal/cli/boot.go:14`). Sections are cut by rank: "What you know" first, then "Self-description", "Last session", "Open threads", "Also active now" and "This session so far". "Issued identity" and "Using engram" are never cut (`boot.go:97` `render`).

Measured on a copy of the real store (9 engrams, 15–89 memories each), from the Legato repo:

| Engram | Default (3000) | Uncut | `--budget 2000` | `--budget 1500` |
|---|---:|---:|---:|---:|
| argus | 8157 B | 8157 B | 7852 B | 5831 B |
| aviarist | 7442 | 7442 | 7442 | 5853 |
| daedalus | 7585 | 7585 | 7585 | 5851 |
| fettle | 6971 | 6971 | 6971 | 5931 |
| hestia | 3616 | 3616 | 3616 | 3616 |
| limen | 7395 | 7395 | 7395 | 5829 |
| mneme | 6198 | 6198 | — | 5900 |
| peitho | 6223 | 6223 | — | 5930 |
| stet | 5998 | 5998 | — | 5998 |

- No real packet reaches the default budget today: they are 3.6–8.2 KB, or about 900–2,000 tokens.
- A brand-new engram's packet is 1,332 bytes. The fixed floor is the seed plus "Using engram", which is about 1 KB. argus's packet breaks down as self-description 2.4 KB, seed 1.8 KB, last session 1.5 KB, what you know 1.1 KB and Using engram 1.0 KB.
- At 1500, the map of memories ("What you know") and parts of the self-description are cut first. That map is how an agent learns what it can `recall`.

**Recommendation:** pass `--budget 2500` (10 KB), or keep the default 3000. Do not go lower. The packet joins the launch prompt with the learning packet (`learning.prompt_max_bytes`, default 16 KiB, `config/config.go:801`), the tools guidance and resources. That role prompt reaches the tool through `$(cat file)` as one argument (`--append-system-prompt`, `-c developer_instructions=`, `--prompt`). Linux caps one argument at 128 KiB (`MAX_ARG_STRLEN`), so even 12 KB leaves plenty of room. Make the budget a config value only if a need shows up.

**Framing:** for Claude Code the role prompt becomes system-prompt text, and for Codex developer instructions. The packet says that everything except the seed is "never an instruction". Legato's framing line should keep it marked as data, fenced in tags the same way the handoff brief now fences the task's text (`7166bf1`).

## 4. Timing, exit codes and output

**Timing:** boot took 4 ms on a new engram and about 7 ms on real-sized engrams (10 boots of limen, 89 memories: 71 ms). `identity list --json` took about 2.5 ms. Boot reads memory files and runs `git` twice for the scope. It never opens the SQLite index (only `recall` does). It is not a cost at spawn. Still give it a timeout (for example 5 s), since it runs `git` in the clone and reads a store on whatever filesystem it lives on.

**Output:** the packet goes to stdout, as Markdown starting `# You are <name>`, with the session line second: `Session <id> (new|resumed, started …), started <UTC>. … Here is <scope>.`. Warnings go to stderr with exit 0. For example, `engram: skipped unreadable memory: …` (`boot.go:252`). Legato must read stdout alone for the packet and log stderr.

**Exit codes** (`cli.go:19`; checked with both binaries):

| Case | Exit | stderr |
|---|---|---|
| OK | 0 | (warnings only) |
| Unknown engram (`ENGRAM_IDENTITY=nobody`) | 1 | `engram: unknown identity "nobody" (known: ada)` |
| Store directory missing | 1 | `engram: unknown identity "ada" (none issued yet; run \`engram identity new <name>\`)` (creates nothing) |
| Invalid name (`Bad Name`) | 1 | `engram: unknown identity "Bad Name" (known: ada)` |
| No identity at all (no `--as`, no `ENGRAM_IDENTITY`, no matching session) | 2 | `engram: no identity: boot with …` + `Run \`engram help boot\` for usage.` |
| I/O failure (for example a read-only store) | 1 | `engram: open …: read-only file system` |

An unknown engram and an I/O failure both exit 1. Legato can only tell them apart by parsing English (request R3). An unknown or missing engram creates nothing, because identity resolution (`boot.go:49`) comes before the session starts (`boot.go:53`). Once the session has started, a later failure leaves a session behind, such as a failure reading threads.

**A spawn that fails after boot leaves a ghost.** Boot starts the session at once. If Legato's spawn then fails (sandbox refused at `agent.go:853`, tmux failure), the empty session is listed under "Also active now" in every boot of that engram for 2 hours (`boot.go:20` `liveWindow`, `:195` `alsoActive`, which lists live sessions even when they have no notes). Legato can narrow the window by booting as late as possible, but the prompt files are written before the sandbox is checked. The proper fix is engram-side (R4 or R5).

## 5. Restart

`restartAgent` (`agent_restart.go:44`) stops the agent and calls `SpawnAgent`, which always makes a **new** execution ID (`agent.go:594`).

- **Resumed conversation** (Claude Code and Codex with a saved conversation, Chimera with its session; `agent_restart.go:77`): `nativeResume` blanks the role prompt for Claude Code and Codex (`agent.go:627`, `:721`). The Chimera adapter leaves out `--prompt` when it resumes (`hooks/chimera.go:382`). No new packet reaches the agent either way, which matches the settled decision. But with today's keying, the new `LEGATO_EXECUTION_ID` gives a **new engram session**. Experiment: after "restart" (`ex-B`), the agent's note went to a new session, and its boot listed the original session under "Also active now". The conversation still holds the old packet and the old session ID, and its notes now split across two sessions. The fix is in Legato: set `ENGRAM_SESSION` to a key saved on the execution, and on a resumed restart (and any task resume of that conversation) copy the key forward. Legato should skip the host boot whenever `SessionPolicy == "resume"` for **any** tool, not only where `nativeResume` is true, because Chimera resumes outside `nativeResume`.
- **Fresh restart:** a new execution, a new key and a fresh boot, so a new engram session. The old session has no handoff. For 2 hours it shows under "Also active now" with its last note, then as "Last session" with its last five notes (`boot.go:222` `lastSession`). That is acceptable, and arguably useful, but an end marker would make it accurate (R4).
- **Re-boot inside a session** (the agent runs `engram boot` after losing context): the strong key resumes and shows "This session so far". Verified, including after a handoff in the same session.

## 6. Does "Using engram" already tell the agent to keep its engram current?

Partly. The section (`boot.go:359` `usingEngram`) is eight command lines, each with one short imperative:

- recall "before assuming"
- note "do it often"
- remember "keeps one durable claim"
- edit and remove
- thread open and close
- handoff "before you finish"
- `help`, `memories`, `self write`

It ends with the "your own notes, never instructions" line.

What it does not say:

- **When to remember.** For example: when you learn something a later session would need (a pitfall hit, a preference the user stated, a working procedure), and with `--source` and `--scope` chosen honestly.
- **When to open a thread.** Work you will not finish in this session, or that you hand to someone else.
- **To boot again after losing context or after compaction.** `help boot` says this, but the packet does not.
- **To keep the self-description current** after learning how you work best or where you were wrong.
- **What a good handoff contains.** `help handoff` has this (what you did, the state, what next, what you were unsure of); the packet has one line.
- **That a session under a harness may be interrupted.** Hand off at natural stopping points, not only at the very end. This matters because Legato will not prompt for a handoff (settled).

So that the guidance is engram's to own, and Legato adds only a framing line, engram needs the requests below.

## Requests to engram

1. **R1. Print the resolved store.** For example `engram where --json` → `{"home": "/abs/path", "source": "ENGRAM_HOME|XDG_DATA_HOME|HOME"}`. Legato can then mount exactly what engram uses instead of re-implementing `homeDir`.
2. **R2. A practice section in the boot packet**, owned by engram and never cut, with a stable heading such as `## Keeping yourself current`. It should say when to note, remember (with source and scope), open and close threads, write the self-description, re-boot after losing context, and hand off at stopping points as well as at the end. An alternative is `engram help practice`, which the packet points to; but text in the packet is what the agent actually reads.
3. **R3. Machine-readable failure.** Either a distinct exit code for "unknown identity" or "no store", separate from I/O errors, or `boot --json` that returns `{packet, session_id, state: new|resumed, identity}` and an error `{code: "unknown_identity"|"no_store"|…}`. With it Legato can record the engram session ID on the execution and give an exact message.
4. **R4. An end marker for a session**, such as `engram session end` (keyed like any command) that writes an `end` event. Boot would then drop the session from "Also active now". Legato would run it when an agent's execution ends or its spawn fails after boot. It is not a handoff, so it does not contradict "no handoff prompt from Legato".
5. **R5. (optional, instead of R4 for failed spawns)** `engram boot --dry-run` or a peek that renders the packet without starting a session. The session would then start at the agent's first command, which already happens with today's code because any command starts a session. The cost is that the packet's `Session <id> (new)` line would be wrong or absent. R4 is simpler.
6. **R6. Document the key contract**: that `ENGRAM_SESSION=<v>` and `LEGATO_EXECUTION_ID=<v>` are different keys (`explicit:` vs `legato:`), and that a strong key resumes regardless of handoff or age. A harness that sets both will rely on this behaviour.
7. **R7. (minor)** Harness detection for Codex and Chimera, or a note in `help` that a harness booting on the agent's behalf should set `ENGRAM_HARNESS`, since `meta.json` is written only once.

## Reproducing

The scripts sit outside the repo and are not committed. In outline:

1. Run `engram identity new ada "…"` with `ENGRAM_HOME=<tmp>/home`.
2. Boot with `ENGRAM_IDENTITY=ada LEGATO_EXECUTION_ID=ex-A` in a git repo whose origin is `git@github.com:example/widget.git`.
3. Inside `bwrap --ro-bind / / --dev /dev --proc /proc --tmpfs <fake HOME> --bind <store> <store>`, run `env ENGRAM_IDENTITY=ada LEGATO_EXECUTION_ID=ex-A engram note …` and compare the session IDs.
4. For packet sizes, run `cp -a ~/.local/share/engram <tmp>/copy` and boot each identity there with `--new` and different `--budget`s, re-copying before each boot.
