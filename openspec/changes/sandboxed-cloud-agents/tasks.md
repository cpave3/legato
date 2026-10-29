Every task is test-first (red, green, refactor). Each phase ends usable on its own; phase 1 needs none of the later ones. Phase 3 is phase 7 of the agent approvals and credentials plan. Phase 4 needs `credential-broker`, `credential-channels` and `github-credentials`.

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
- [ ] 3.3 Mount plan in the service: working directory, cache, agent tool state per adapter, broker socket, the execution's SSH agent socket when one is open, `legato` binary, pasted-image folder, resources, configured mounts; and the paths that must never be mounted; tests
- [ ] 3.4 Wrap each adapter's launch command when the sandbox is on; refuse the spawn before tmux when the backend binary is missing; tests per adapter
- [ ] 3.5 Strip credential variables from the sandbox environment whatever the host process holds; test
- [ ] 3.6 Integration test (skipped without bwrap): inside the sandbox the config, auth token, `~/.config/gh` and `~/.ssh` do not exist, the cache is read-only, the working copy is writable, and every process ends with the pane
- [ ] 3.7 Verify each agent tool inside bubblewrap (Claude Code, Codex, Chimera): starts, its own sandbox setting, native bridge binds through the broker, MCP tools work; record results in the design's Open Questions
- [ ] 3.8 `internal/engine/gitrepo`: bare clone and fetch with a read token from the credential broker, passed through an inherited file descriptor and an askpass helper; tests against a local bare repository served over a file URL, and a test that the token is in no argument, environment variable or file
- [ ] 3.9 `RepositoryService.Ensure`: cache path, `max_age`, per-repository serialization, clean failure with no partial directory; concurrency test
- [ ] 3.10 Job clones: `git clone --reference`, checkout of ref or default branch, `origin` left at the GitHub URL; migration for the job clone table; test that with no `github` credential `git push` from the clone goes to the placeholder push URL and fails without network
- [ ] 3.11 Implement `repo.ensure` with the cache; let presets with `repo` and no `primary_dir` use a job clone; record code context
- [ ] 3.12 Job clone cleanup on terminal executions (remove when nothing is unpushed, keep 7 days otherwise with a task note); tests
- [ ] 3.13 Docs: `docs/ai/config.md` (new keys), sandbox section in `docs/cloud-agents.md`

## 4. Phase 4: repository tools, with pushes and pull requests through the credential broker

- [ ] 4.1 Broker operations `git.ensure_repo` and `git.refresh_repo`; MCP tools `ensure_repo` and `refresh_repo`, listed only when a `github` credential is configured; add them to `MCPToolNames` and `MCPGuidance`; tests per tool
- [ ] 4.2 Host git on job clones (cleanup check, sibling checkouts) runs with `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, hooks and fsmonitor off and no credential; tests that plant `pre-push`, `core.hooksPath`, `core.fsmonitor`, `core.sshCommand`, `credential.helper` and `uploadpack.packObjectsHook` in a job clone and assert none runs
- [ ] 4.3 Sandboxed sessions get the `helper` channel's variables and reach the credential proxy on loopback; test that the sandbox environment has the `pushInsteadOf` entries and no write token
- [ ] 4.4 A push from a job clone reaches the proxy, and the `github` profile's rules decide it: `legato/<task-id>-<slug>` after `request_access` is pushed, `main` is refused with git's `remote:` message; integration test against a local smart-HTTP remote
- [ ] 4.5 A push is recorded in the credential audit log and as a task note naming the repository, branch, old and new tip; service test
- [ ] 4.6 `pr.create` through the proxy links the new pull request to the task's PR tracking as pinned; test against a fake GitHub API
- [ ] 4.7 End-to-end test against a local bare "remote" and a fake GitHub API: a sandboxed agent commits, asks for access, pushes and opens a pull request, and the task shows the PR
- [ ] 4.8 Docs: `docs/ai/agent-mcp.md` (repository tools), `docs/ai/pr-tracking.md` (the changed boundary), `docs/cloud-agents.md` (a GitHub App credential on the VM, replacing the fine-grained token for pushes)
- [ ] 4.9 Update the `github-pr-client` main spec when the change is archived
