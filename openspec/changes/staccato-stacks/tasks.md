## 1. Push path for stacks (engine, service)

- [x] 1.1 Forced updates are classified `git.push`: already covered by `TestEachRefUpdateIsAnOperation` (classification ignores fast-forward); no new test
- [x] 1.2 Test in the broker: a session grant on `github:o/r@refs/heads/legato/t1-*` covers pushes of `legato/t1-a` and `legato/t1-part-2` in one push, not `legato/t1-a/b` nor `legato/t2-a`; the request for the pattern is decided by the default `legato/**` rule and marked broader scope. Done when it passes
- [x] 1.3 Test through the proxy (`httpproxy_git` tests): one push of three `legato/t1-*` refs, two of them forced, with the pattern grant, is allowed with one token and no approval

## 2. Guidance (cli, hooks)

- [x] 2.1 Test `cli`: a `StackSupport` probe that runs `st graph import --help` with a 2-second limit; reports true on exit 0, false on non-zero, on a missing `st` and on a timeout (with a fake `st` script on a temporary `PATH`)
- [x] 2.2 Implement the probe; cache it per process (MCP server) and for one minute on the host
- [x] 2.3 Test `MCPGuidance` and `CredentialGuidance`: with the probe true, the stack paragraph appears with the session's task ID; with a `github` credential too, the pattern request replaces "ask for one branch, not a pattern" for stacks; with the probe false, nothing about Staccato
- [x] 2.4 Write the stack paragraph and the GitHub guidance change; wire the probe into `legato mcp serve` and the Codex/Chimera launch prompt (`describeCredentials`)
- [x] 2.5 Role prompts left as they are: swarm agents get the stack guidance through the MCP instructions and launch prompt like any agent

## 3. Reading stacks (engine)

- [x] 3.1 Not needed: `stackgraph` reads refs itself through `os.Root.FS()`, so `gitrepo` exports nothing new
- [x] 3.2 Tests for `internal/engine/stackgraph`: reads a graph and tips from a real `git init` clone; `pushed`/`unpushed`/`missing`; invalid names (`../x`, `a..b`, `-x`, `x.lock`, control characters) reported and not opened; no `graph.json` is no stack; a `refs/staccato/` ref with no file is unsupported; a `graph.json` over 1 MiB or unparsable is a problem; `core.fsmonitor` set to a command that writes a file is never run
- [x] 3.3 Implement `stackgraph.Read(gitDir *os.Root / path, readRef func)` and the branch-name check
- [x] 3.4 Tests for the document: format-1 fields, depth-first order with siblings by name, offered only when all branches are pushed and valid
- [x] 3.5 Implement `stackgraph.Document`
- [x] 3.6 Integration test (skipped unless `st graph export --help` exits 0): build a stack with `st` in a temporary repository with a bare origin, push it, and compare Legato's document with `st graph export --all` apart from `st_version`

## 4. Recording stacks (store, service)

- [x] 4.1 Migration `081_clone_stacks.sql`: `clone_stacks(clone_id PK, root, branches JSON, problem, read_at)`; store methods `PutCloneStack`, `DeleteCloneStack`, `CloneStackByID`, `ListTaskStacks` (through `job_clone_tasks`), `ListLiveTaskJobClones`; tests with a real SQLite database
- [x] 4.2 Tests for `TaskStackService`: `Record(clone)` stores a stack, replaces it on a second read, stores nothing for a clone with no stack; `List(task)` re-reads live clones and returns records for removed ones
- [x] 4.3 Implement `internal/service/task_stacks.go`
- [x] 4.4 Tests: `EventAgentDied` for an execution with a job clone records its stack; `RemoveJobClone` and `SweepJobClones` record before removing (the record outlives the directory)
- [x] 4.5 Wire the triggers: subscribe to `EventAgentDied`; a before-remove hook on `RepositoryService`; wire in `cmd/legato/repository_runtime.go`
- [x] 4.6 Update `docs/ai/database.md` for the table

## 5. API and web UI

- [x] 5.1 Server tests: `GET /api/tasks/{id}/stacks` shape; `…/stacks/{clone}/document` attachment name and body, `409` naming the unpushed branch, `404` for an unknown task or clone, `401` without a token
- [x] 5.2 Implement `internal/server/task_stacks.go` and wire it for both web servers
- [x] 5.3 Web tests (`TaskStacks.test.tsx`): tree with states, **Download** and the copyable `st graph import` command only when importable, the reason when not, "clone removed" for a record without its clone
- [x] 5.4 Implement `web/src/components/board/TaskStacks.tsx` and add it to `TaskActivityTabs`; `pnpm exec tsc -b` and the web tests pass
- [x] 5.5 Update `docs/ai/web-ui.md` and `site/…/using/web-ui.md`

## 6. Updating st (service, server, web)

- [x] 6.1 Tests for the tool updater with fake `st` scripts: missing; `release: false` is unmanaged and never updated; check parses `--check --json`; `auto` installs at once; `ask` waits for apply; `off` runs nothing; a failing `st update` sets `failed` with its last line and leaves Legato's own state alone; 2-minute limit
- [x] 6.2 Implement `internal/service/update_tools.go` and run it from `UpdateService.check` and `Run`; `UpdateStatus.Tools`
- [x] 6.3 Server tests: `tools` in `GET /api/updates` and `/api/settings`; `POST /api/updates/tools/st/apply` `202`, `409` unmanaged or nothing newer
- [x] 6.4 Implement the endpoint; the `updates` Attention producer's `st` item in `ask` mode, with a test
- [x] 6.5 Web: `st` row in `UpdatesSettings.tsx` with **Update st** in `ask` mode; tests
- [x] 6.6 Update `docs/ai/updates.md` and `site/…/running/updates.md`

## 7. Documentation and checks

- [x] 7.1 `docs/ai/credentials.md` § GitHub (guidance, pattern grant for stacks) and `docs/ai/cli.md` § Staccato Integration (stack guidance, recorded stacks, import by download)
- [x] 7.2 `site/…/integrations/staccato.md`: stacks on a worker, start to finish; `site/…/running/agent-server.md`: the `curl` line for `st` beside `legato`
- [x] 7.3 `docs/ai/packages.md`: `internal/engine/stackgraph`, `task_stacks.go`, `update_tools.go`
- [x] 7.4 Scratch instance (own config, data, runtime folder and port; no tmux server started) with a local origin instead of GitHub: a three-branch stack built with real `st` v0.1.0 in a job clone; the stacks API showed it unpushed with `409` on the document; after `st sync` it was importable, the document downloaded as `<task>-widget-stack.json`, and `st graph import` added all three branches with their parents in a second clone; `GET /api/updates` reported the release `st` as managed and up to date; `legato mcp serve` gave the stack guidance; after `DELETE /api/job-clones/1` the stack stayed listed (`live: false`) and downloadable. Instance stopped and folders removed. Not run: a real agent and real GitHub (one approval for a stack is covered by `TestOneGrantOnTheTasksBranchPatternCoversAWholeStack`, real git through the proxy to a fake GitHub)
- [x] 7.5 Checks pass: `go build`, `go vet` and `go test -short ./...`; golangci-lint finds nothing in the changed files (its remaining findings predate this change); web tests (1137) and `pnpm exec tsc -b`; the docs site builds with its link validator. `task check` itself not run, since its build step writes `./legato`
