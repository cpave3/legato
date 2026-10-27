Every task is test-first (red, green, refactor). Each phase ends usable on its own; phase 1 needs none of the later ones.

## 1. Phase 1: job submission and a VM as the sandbox

- [ ] 1.1 Add `prompt` to the `POST /api/agents/spawn` request; test that a prompt reaches the agent service and that a blank prompt is ignored
- [ ] 1.2 Add `prompt` to `AgentSpawnOptions` and deliver it after launch through the kickoff path (native endpoint when bound, typed otherwise); service tests with `mockTmux` for both paths
- [ ] 1.3 Add `prompt` to `POST /api/tasks/{id}/agent`: message the live agent when one is selected, deliver as first message on resume without the fresh-session kickoff
- [ ] 1.4 Add the `repo.ensure` workflow executor as a no-op when `repo` is empty (the cache arrives in phase 3)
- [ ] 1.5 Ship the built-in `legato-job` workflow definition (inputs `prompt`, `repo`, `ref`, `agent`); validate it in tests and check an unknown `agent` is rejected at start
- [ ] 1.6 Show `prompt` in the web spawn dialog as an optional first message (`SpawnAgentDialog`); component test
- [ ] 1.7 Write `docs/cloud-agents.md`: VM setup (systemd unit for `legato serve`, tmux, agent tool logins without a browser, Tailscale, fine-grained token, branch protection), adding the VM as a second server in the web and phone apps, starting a job with `curl`
- [ ] 1.8 Run the spike on a scratch VM or scratch instance: start `legato-job` by API, watch it in the native panel, get an ntfy notification; record what hurt in the change's design Open Questions

## 2. Phase 2: host broker

- [ ] 2.1 Migration: `executions.broker_token_hash`; store methods to set it and to resolve a hash to a running execution; tests for terminal executions resolving to nothing
- [ ] 2.2 `internal/engine/broker`: socket server (request/reply, newline-delimited JSON, stale-socket removal, mode 0600) and client (2-second timeout); tests with a real socket in `t.TempDir()`
- [ ] 2.3 Mint a token at spawn, store its hash, put `LEGATO_BROKER_SOCKET` and `LEGATO_BROKER_TOKEN` in the agent environment; test the database never holds the token
- [ ] 2.4 `service.Broker`: dispatcher that resolves the token, rejects other identities (`forbidden`), unknown ops (`unknown_op`) and terminal executions (`unauthorized`)
- [ ] 2.5 Broker operations for activity: `agent.state`, `agent.stopped`, `agent.session_created`, `agent.permission_resolved`, calling the same service code as the CLI commands
- [ ] 2.6 Broker operations for bridges: `endpoint.bind`, `endpoint.unbind`, `resume.save`
- [ ] 2.7 Broker operations for MCP tools: `ui.show` (images inline up to 25 MiB; videos by host-visible path), `task.context`, `task.note`, `peers.list`, `peers.message`, `peers.broadcast`
- [ ] 2.8 Start the broker in both `legato serve` and the TUI runtime; shut it down cleanly
- [ ] 2.9 A `brokerclient` mode for the hook commands: use the broker when both variables are set and then open neither store nor config; exit 0 with a stderr note when unreachable; contract tests run each command against store mode and broker mode
- [ ] 2.10 Broker mode for `legato mcp serve`; tool errors when unreachable; contract tests per tool
- [ ] 2.11 Broker mode for `legato agent bridge` and `legato agent codex-bridge` (bind, unbind, resume identity)
- [ ] 2.12 Architecture test: in broker mode none of these commands imports a path that opens the store (or a runtime check that the database file is never opened)
- [ ] 2.13 Docs: broker section in `docs/ai/cli.md` and `docs/ai/packages.md`

## 3. Phase 3: sandbox launcher and repository cache

- [ ] 3.1 Config: `agents.sandbox` (`enabled`, `backend`, `mounts`), `repositories.cache.max_age`; validation and tests
- [ ] 3.2 `internal/engine/sandbox`: backend interface and `bwrap` implementation that builds the argument list from a mount plan; table tests of the arguments for each mount kind
- [ ] 3.3 Mount plan in the service: working directory, cache, agent tool state per adapter, broker socket, `legato` binary, pasted-image folder, resources, configured mounts; and the paths that must never be mounted; tests
- [ ] 3.4 Wrap each adapter's launch command when the sandbox is on; refuse the spawn before tmux when the backend binary is missing; tests per adapter
- [ ] 3.5 Strip credential variables from the sandbox environment whatever the host process holds; test
- [ ] 3.6 Integration test (skipped without bwrap): inside the sandbox the config, auth token, `~/.config/gh` and `~/.ssh` do not exist, the cache is read-only, the working copy is writable, and every process ends with the pane
- [ ] 3.7 Verify each agent tool inside bubblewrap (Claude Code, Codex, Chimera): starts, its own sandbox setting, native bridge binds through the broker, MCP tools work; record results in the design's Open Questions
- [ ] 3.8 `internal/engine/gitrepo`: bare clone and fetch with a credential passed through an inherited file descriptor and an askpass helper; tests against a local bare repository served over a file URL, and a test that the credential is in no argument, environment variable or file
- [ ] 3.9 `RepositoryService.Ensure`: cache path, `max_age`, per-repository serialization, clean failure with no partial directory; concurrency test
- [ ] 3.10 Job clones: `git clone --reference`, checkout of ref or default branch, placeholder `origin`; migration for the job clone table; test that `git push` from the clone fails without network
- [ ] 3.11 Implement `repo.ensure` with the cache; let presets with `repo` and no `primary_dir` use a job clone; record code context
- [ ] 3.12 Job clone cleanup on terminal executions (remove when nothing is unpushed, keep 7 days otherwise with a task note); tests
- [ ] 3.13 Docs: `docs/ai/config.md` (new keys), sandbox section in `docs/cloud-agents.md`

## 4. Phase 4: git broker tools and the pull-request writer

- [ ] 4.1 Config: `github.write` (`repositories`, `branch_prefix`, `protected`, `unsandboxed`); validation and tests
- [ ] 4.2 `github.PRWriter` with its own transport guard (only `POST /repos/{o}/{r}/pulls` for allowlisted repositories); tests that other methods, paths and repositories dispatch zero requests; the read client's GET-only tests stay unchanged
- [ ] 4.3 Settle the untrusted-fetch question: tests that plant `pre-push`, `core.hooksPath`, `core.fsmonitor`, `core.sshCommand`, `credential.helper` and `uploadpack.packObjectsHook` in a job clone and assert none runs during import; switch to `git bundle` if any does
- [ ] 4.4 Branch import: read the tip from ref files without running git in the job clone, fetch into the host-owned bare repository with no hooks and no system or global config, `fsck --connectivity-only`
- [ ] 4.5 Push policy (allowlist, prefix, protected and default branches, lease for force pushes, 50 MiB file limit) as a pure function with table tests
- [ ] 4.6 `GitBrokerService.Push`: import, policy, push with the credential; push log migration; task note on success
- [ ] 4.7 `GitBrokerService.OpenPR`: only for branches pushed for this task; return an existing open PR; pin it to the task's PR tracking
- [ ] 4.8 Broker operations `git.ensure_repo`, `git.refresh_repo`, `git.push_branch`, `git.open_pr`
- [ ] 4.9 MCP tools `ensure_repo`, `refresh_repo`, `push_branch`, `open_pr` in `legato mcp serve`, listed only under the conditions in the spec; add them to `MCPToolNames` and to `MCPGuidance`
- [ ] 4.10 End-to-end test against a local bare "remote" and a fake GitHub API: a sandboxed agent commits, calls `push_branch` and `open_pr`, and the task shows the PR
- [ ] 4.11 Docs: `docs/ai/agent-mcp.md` (git tools), `docs/ai/pr-tracking.md` (the writer and the changed boundary), `docs/cloud-agents.md` (enabling writes)
- [ ] 4.12 Update the `github-pr-client` main spec when the change is archived
