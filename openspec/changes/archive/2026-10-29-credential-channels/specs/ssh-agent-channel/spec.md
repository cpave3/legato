## ADDED Requirements

### Requirement: One agent socket for each execution
When the configuration has a credential that uses the `ssh` profile, Legato SHALL open an SSH agent socket for each execution at `<runtime dir>/legato/ssh/<execution_id>.sock` with mode `0600` in a directory with mode `0700`, and SHALL set `SSH_AUTH_SOCK` to it in the agent's session environment. The socket SHALL be removed when the execution becomes terminal.

#### Scenario: Agent session
- **WHEN** an agent is spawned and an `ssh` credential is configured
- **THEN** the session's `SSH_AUTH_SOCK` SHALL be the execution's socket

#### Scenario: Execution ends
- **WHEN** the execution becomes terminal
- **THEN** the socket file SHALL no longer exist

#### Scenario: No ssh credential
- **WHEN** no credential uses the `ssh` profile
- **THEN** Legato SHALL open no socket and the session SHALL keep the `SSH_AUTH_SOCK` it inherits

### Requirement: Only the credentials' keys, and no changes
The socket SHALL list only the public keys of the `ssh` credentials, SHALL sign only with them, and SHALL refuse every request that adds, removes or locks keys. It SHALL NOT pass a request on to another agent.

#### Scenario: Listing identities
- **WHEN** a client sends `REQUEST_IDENTITIES`
- **THEN** the answer SHALL hold the public keys of the `ssh` credentials and no other key

#### Scenario: Adding a key
- **WHEN** a client sends `ADD_IDENTITY`
- **THEN** the socket SHALL answer `SSH_AGENT_FAILURE`

### Requirement: Session binding is required
The socket SHALL accept the `session-bind@openssh.com` extension, SHALL verify the server's signature over the session identifier with the host key it carries, and SHALL keep the binding for that agent connection. It SHALL refuse a sign request on a connection that has no valid binding, or whose data names another session identifier.

#### Scenario: Bound login
- **WHEN** OpenSSH binds a connection to a host key and then asks for a user authentication signature for the same session
- **THEN** the socket SHALL authorize the use and sign when it is within a grant

#### Scenario: No binding
- **WHEN** a client asks for a signature without a session bind
- **THEN** the socket SHALL answer `SSH_AGENT_FAILURE` and SHALL record the refusal

#### Scenario: Forged binding
- **WHEN** the server's signature in the bind does not verify with the host key
- **THEN** the socket SHALL refuse the bind and every later sign request on that connection

### Requirement: The ssh inspector
The `ssh` inspector SHALL accept only sign data that is a `publickey` user authentication request for the `ssh-connection` service. It SHALL take the remote user from that data and the host from the bound host key, resolved to host names through the configured `known_hosts` file. It SHALL produce the operation `ssh.login` and the resource `ssh://<user>@<host>`, with `:<port>` when the port is not 22. A host key with no `known_hosts` entry SHALL produce the host `key:<SHA256 fingerprint>`.

#### Scenario: Known host
- **WHEN** the bound host key is in `known_hosts` as `nas.lan` and the sign data names user `cameron`
- **THEN** the use SHALL be `ssh.login` on `ssh://cameron@nas.lan`

#### Scenario: Unknown host key
- **WHEN** the bound host key has no entry
- **THEN** the resource SHALL be `ssh://cameron@key:SHA256:<fingerprint>`

#### Scenario: Data that is not a login
- **WHEN** the sign data is not a user authentication request
- **THEN** the socket SHALL refuse it

### Requirement: Holding a sign request
When a sign request needs the person, the socket SHALL hold it for up to 60 seconds. It SHALL sign when the person approves in that time. Otherwise it SHALL answer `SSH_AGENT_FAILURE`, leave the approval request open, and Legato SHALL send the agent a `[legato approvals]` message when the person decides.

#### Scenario: Approved in time
- **WHEN** the person approves 20 seconds after the sign request
- **THEN** the socket SHALL sign and the login SHALL continue

#### Scenario: No decision in time
- **WHEN** 60 seconds pass with no decision
- **THEN** the socket SHALL answer `SSH_AGENT_FAILURE`
- **AND** the approval request SHALL stay open

#### Scenario: Decided later
- **WHEN** the person approves after the socket refused
- **THEN** the agent SHALL receive `[legato approvals] ap-… approved` and a new login SHALL be authorized

### Requirement: Forwarding denied by default
A sign request on a connection whose session bind has the forwarding flag set SHALL be the operation `ssh.forwarded-login`. The `ssh` profile SHALL deny it by default.

#### Scenario: ssh -A
- **WHEN** a remote host asks for a signature through a forwarded agent
- **THEN** the use SHALL be `ssh.forwarded-login` and the default rule SHALL deny it

### Requirement: The ssh profile
Legato SHALL ship a profile named `ssh` that takes a `keypair` secret, uses the `ssh-agent` channel and the `ssh` inspector, and has the default rules: `ssh.login` on any host `window` of 1 hour for each host, and `ssh.forwarded-login` `deny`.

#### Scenario: Second login within the window
- **WHEN** the person approved `ssh://cameron@nas.lan` with a 1-hour window 10 minutes ago
- **THEN** a new login to that host SHALL be signed without a new approval request

### Requirement: Legato's own processes keep their agent
`config.applySSHAgent` SHALL keep choosing `SSH_AUTH_SOCK` for Legato's own process as before. When an `ssh` credential is configured, the execution's socket SHALL replace that value in agent sessions only.

#### Scenario: Host-side git
- **WHEN** Legato itself runs git with 1Password's socket chosen by `applySSHAgent`
- **THEN** that git process SHALL use 1Password's socket, not an execution's socket
