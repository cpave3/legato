## ADDED Requirements

### Requirement: Sandboxed launch
When `agents.sandbox.enabled` is true, the agent service SHALL start every adapter-launched agent (Claude Code, Codex, Chimera, and a shell) inside a sandbox built by the configured backend (`bwrap` in this change). The tmux session and pane SHALL stay on the host; the agent tool and its children SHALL run inside the sandbox. When it is false, launches SHALL be unchanged.

#### Scenario: Sandbox on
- **WHEN** an agent is spawned with the sandbox on
- **THEN** the command typed into the pane SHALL start the adapter's launch command through the sandbox backend
- **AND** the agent's environment SHALL include `LEGATO_BROKER_SOCKET` and `LEGATO_BROKER_TOKEN`

#### Scenario: A sandbox with many mounts
- **WHEN** the sandbox's options would make the typed launch command longer than a terminal line (4096 bytes)
- **THEN** Legato SHALL write the options to a file of the execution's (`<runtime>/legato/sandbox/<execution>/bwrap.args`, mode `0600`) and type only the backend, the file to read them from, and the launch command
- **AND** the file SHALL not stay open in the agent's processes

#### Scenario: Sandbox off
- **WHEN** `agents.sandbox.enabled` is false or absent
- **THEN** the launch command SHALL be exactly what it is without this change

#### Scenario: Backend missing
- **WHEN** the sandbox is on and the backend binary is not installed
- **THEN** the spawn SHALL fail before any tmux session is created, with an error naming the missing binary
- **AND** Legato SHALL NOT fall back to an unsandboxed launch

### Requirement: What a sandbox can see
A sandbox SHALL mount: system directories read-only; a private `/tmp`; the job's working directory read-write; the repository cache read-only at its host path; the agent tool's own state directory read-write; the broker socket; the execution's SSH agent socket when one is open; the `legato` binary read-only; this task's pasted-image folder read-only; resources given to the agent with their configured access; and `agents.sandbox.mounts` with their configured access. It SHALL NOT mount the Legato configuration file, the data directory (except the repository cache and job clones), `~/.config/gh`, `~/.ssh`, `~/.gitconfig`, or the git credential store.

#### Scenario: Credentials are absent
- **WHEN** a command inside the sandbox reads the Legato configuration path, the auth token path, `~/.config/gh/hosts.yml` or `~/.ssh`
- **THEN** each read SHALL fail because the path does not exist inside the sandbox

#### Scenario: Sibling repositories are readable
- **WHEN** a command inside the sandbox runs `git -C <data_dir>/repos/acme/other.git log -1`
- **THEN** it SHALL succeed
- **AND** writing under that path SHALL fail

#### Scenario: Working copy is writable
- **WHEN** the agent commits in its job checkout
- **THEN** the commit SHALL succeed and be visible to the host at the same path

#### Scenario: Environment holds no credentials
- **WHEN** the sandbox's environment is listed
- **THEN** it SHALL contain no `GITHUB_TOKEN`, `GH_TOKEN`, `LEGATO_GITHUB_TOKEN`, Jira token or web auth token, whatever the host process's environment holds

### Requirement: Native endpoints stay reachable
A sandbox SHALL share the host's network namespace so that a bridge listening on loopback inside it can be reached by the host Legato.

#### Scenario: Bridge binds from inside the sandbox
- **WHEN** a sandboxed Claude Code session starts its bridge
- **THEN** the bridge SHALL bind through the broker
- **AND** the host's web UI SHALL show the session in the native panel

### Requirement: Sandbox lifetime
The sandbox SHALL end when the pane's process ends, and every process inside it SHALL end with it.

#### Scenario: Agent killed
- **WHEN** the agent's tmux session is killed
- **THEN** no process started inside its sandbox SHALL remain running

### Requirement: An agent can be spawned without the sandbox
With the sandbox on, the person SHALL be able to start one agent without it, from the spawn dialogs of the web UI and the TUI (**Run without sandbox**) and with `sandbox: false` on `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent`. Such an agent SHALL launch as it would with the sandbox off, and SHALL be marked `unsandboxed` wherever its agent tool is named. The choice SHALL NOT be remembered from one spawn to the next. Swarm workers and workflow agents SHALL always be sandboxed while the sandbox is on.

#### Scenario: Opting out
- **WHEN** the person ticks **Run without sandbox** and spawns an agent
- **THEN** the launch command SHALL not go through the sandbox backend
- **AND** the agent list SHALL mark the agent `unsandboxed`

#### Scenario: Next spawn
- **WHEN** the dialog opens again
- **THEN** **Run without sandbox** SHALL be unticked

### Requirement: Resources added to a running sandboxed agent
A sandbox's mounts SHALL be fixed when it starts. Adding a resource to a running sandboxed agent SHALL be refused (`409`) with a message saying why, unless the request asks to reload the agent; the agent SHALL then be restarted with its resources and the new ones, resuming its conversation.

#### Scenario: Attach to a sandboxed agent
- **WHEN** the person adds a resource to a running sandboxed agent without asking to reload
- **THEN** Legato SHALL refuse it and say why, and SHALL NOT tell the agent it has the paths

#### Scenario: Attach with a reload
- **WHEN** the request asks to reload (`{names, reload: true}`)
- **THEN** Legato SHALL restart the agent in a sandbox that mounts the new resources, resuming its conversation

### Requirement: Toolchains are visible
A sandbox SHALL mount, read-only and at their host paths when they exist, the person's toolchain directories: `~/.local/bin`, `~/.local/share/mise`, `~/go/bin`, `/home/linuxbrew/.linuxbrew`, `~/.cargo/bin`, `~/.rustup` and `~/.bun`; and read-write the package and build caches `~/go/pkg/mod`, `~/.cache/go-build`, `~/.npm`, `~/.cache/pnpm`, `~/.local/share/pnpm` and `~/.cache/pip`. `agents.sandbox.default_mounts: false` SHALL turn these off. `agents.sandbox.mounts` SHALL add to them.

#### Scenario: A tool linked from its checkout
- **WHEN** `~/.local/bin/st` is a link to `~/Projects/staccato/st` and the sandbox mounts `~/.local/bin`
- **THEN** the sandbox SHALL mount `~/Projects/staccato/st` read-only, and no other file of that folder
- **AND** a link whose target is in a path a sandbox must not see SHALL stay broken

#### Scenario: Running the project's tests
- **WHEN** a sandboxed agent runs `go test ./...` or `pnpm test` in its working copy
- **THEN** the tools SHALL be found on its `PATH` and the module and package caches SHALL be usable


### Requirement: Each agent sees only its own tool
A sandbox SHALL mount, read-only, the installation of the agent tool the agent runs (where its command's link leads, or the checkout a launcher script runs), and SHALL NOT mount another tool's installation. It SHALL mount, read-write, the tool's state folders (for Claude Code `~/.claude`, `~/.claude.json` and its cache, data and state folders; for Codex `~/.codex`; for Chimera `~/.chimera` and its configuration and data folders), and also a state folder that `CLAUDE_CONFIG_DIR` or `CODEX_HOME` moves elsewhere: the session's value, such as a resumed session's storage folder, else the one Legato inherited. A missing moved folder SHALL be made on the host first. A moved folder inside a path a sandbox must not see SHALL fail the spawn.

#### Scenario: A Chimera checkout and a Claude Code agent
- **WHEN** Chimera is installed from a checkout and a Claude Code agent is spawned in another project
- **THEN** the Claude Code agent's sandbox SHALL NOT contain the Chimera checkout

#### Scenario: Resume across the sandbox
- **WHEN** a session started outside the sandbox is resumed inside it, or the other way round
- **THEN** the agent tool SHALL find the conversation, because both read and write the same state folders on the host

#### Scenario: A moved state folder
- **WHEN** `CLAUDE_CONFIG_DIR` names a folder outside `~/.claude`
- **THEN** a sandboxed Claude Code agent SHALL see that folder read-write at its host path

### Requirement: ssh runs inside the sandbox
Because the sandbox's user namespace shows files root owns as owned by nobody, which ssh refuses as configuration, a sandbox SHALL be given copies of the system ssh configuration (`/etc/ssh/ssh_config` and every file it includes, followed as ssh follows `Include`) owned by the person, each mounted over the real path of its original. The copy of `ssh_config` SHALL name, as `UserKnownHostsFile`, the file `credential_channels.ssh_agent.known_hosts` names, mounted read-only. `~/.ssh` SHALL stay hidden.

#### Scenario: ssh in a sandbox
- **WHEN** a sandboxed agent runs `ssh -G host`
- **THEN** ssh SHALL read its configuration without an ownership error
- **AND** SHALL use the mounted known hosts file
- **AND** the person's private keys SHALL not be readable

### Requirement: gh in the sandbox is Legato's wrapper
With a `github` credential, a sandbox SHALL mount Legato's `gh` wrapper over every real `gh` on the host's `PATH` (links resolved), and the real `gh` at a path of the execution's named by `LEGATO_GH_REAL`, which the wrapper runs. Whatever order a login shell puts `PATH` in, `gh` SHALL be the wrapper. When the wrapper gets no token it SHALL print Legato's reason. An agent outside the sandbox SHALL keep the person's own `gh` login: no stand-in `GH_TOKEN` and no empty `GH_CONFIG_DIR`.

#### Scenario: A login shell puts Homebrew first
- **WHEN** a sandboxed agent's login shell puts Homebrew's `bin` ahead of the wrapper's folder and runs `gh pr view 1 -R acme/widget`
- **THEN** the wrapper SHALL run, give the real `gh` a read token, and run the real `gh`

#### Scenario: No token
- **WHEN** Legato gives the wrapper no token
- **THEN** the wrapper SHALL print Legato's reason before the real `gh` runs

#### Scenario: Outside the sandbox
- **WHEN** an agent is spawned without the sandbox
- **THEN** its environment SHALL have neither the stand-in `GH_TOKEN` nor Legato's empty `GH_CONFIG_DIR`

### Requirement: GitHub fetches over ssh URLs in the sandbox
With a `github` credential, a sandboxed session's git settings SHALL send fetches from `git@github.com:` and `ssh://git@github.com/` to `https://github.com/`, where Legato's credential helper gives read tokens. Pushes from those forms SHALL still go to the credential proxy. An unsandboxed session SHALL keep fetching over ssh.

#### Scenario: A clone with an ssh origin
- **WHEN** a sandboxed agent runs `git fetch` in a clone whose `origin` is `git@github.com:acme/widget.git`
- **THEN** git SHALL fetch from `https://github.com/acme/widget.git` with a read token
- **AND** `git push` SHALL still go to the proxy
