## ADDED Requirements

### Requirement: Sandboxed launch
When `agents.sandbox.enabled` is true, the agent service SHALL start every adapter-launched agent (Claude Code, Codex, Chimera, and a shell) inside a sandbox built by the configured backend (`bwrap` in this change). The tmux session and pane SHALL stay on the host; the agent tool and its children SHALL run inside the sandbox. When it is false, launches SHALL be unchanged.

#### Scenario: Sandbox on
- **WHEN** an agent is spawned with the sandbox on
- **THEN** the command typed into the pane SHALL start the adapter's launch command through the sandbox backend
- **AND** the agent's environment SHALL include `LEGATO_BROKER_SOCKET` and `LEGATO_BROKER_TOKEN`

#### Scenario: Sandbox off
- **WHEN** `agents.sandbox.enabled` is false or absent
- **THEN** the launch command SHALL be exactly what it is without this change

#### Scenario: Backend missing
- **WHEN** the sandbox is on and the backend binary is not installed
- **THEN** the spawn SHALL fail before any tmux session is created, with an error naming the missing binary
- **AND** Legato SHALL NOT fall back to an unsandboxed launch

### Requirement: What a sandbox can see
A sandbox SHALL mount: system directories read-only; a private `/tmp`; the job's working directory read-write; the repository cache read-only at its host path; the agent tool's own state directory read-write; the broker socket; the `legato` binary read-only; this task's pasted-image folder read-only; resources given to the agent with their configured access; and `agents.sandbox.mounts` with their configured access. It SHALL NOT mount the Legato configuration file, the data directory (except the repository cache and job clones), `~/.config/gh`, `~/.ssh`, `~/.gitconfig`, or the git credential store.

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
